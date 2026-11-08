package handler

import (
	"context"
	"errors"
	"strings"

	"github.com/MACOS-DO/sub4api/internal/pkg/ctxkey"
	"github.com/MACOS-DO/sub4api/internal/pkg/requestmodel"
	middleware "github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type smartWSRouteSelection struct {
	selection *service.AccountSelectionResult
	decision  service.OpenAIAccountScheduleDecision
}

func (s *smartWSRouteSelection) release() {
	if s != nil && s.selection != nil && s.selection.ReleaseFunc != nil {
		s.selection.ReleaseFunc()
	}
	if s != nil {
		s.selection = nil
	}
}

// Select once from the first response.create before group-dependent admission.
// Nothing has been sent upstream. After the route is chosen, the existing WS
// handler owns all billing, reservations, privacy policy and connection affinity.
func (h *OpenAIGatewayHandler) selectSmartWebSocketRoute(c *gin.Context, key *service.APIKey, model string, body []byte) (*service.APIKey, *smartWSRouteSelection, error) {
	if len(key.CandidateGroupIDs()) < 2 {
		return key, nil, nil
	}
	if h.apiKeyService == nil || h.gatewayService == nil {
		return nil, nil, service.ErrSchedulerCacheNotReady
	}
	selected, held, ctx, err := selectSmartWSRoute(c.Request.Context(), key, model, body, h.apiKeyService.BindAPIKeyRequestGroup,
		func(ctx context.Context, candidate *service.APIKey) (context.Context, string, error) {
			platform := candidate.Group.Platform
			if platform == service.PlatformComposite {
				if h.compositeResolver == nil {
					return ctx, "", service.ErrSchedulerCacheNotReady
				}
				decision, err := h.compositeResolver.Resolve(ctx, candidate.Group.ID, model, service.CompositeRouteEndpointResponses)
				if err != nil {
					return ctx, "", err
				}
				if !decision.Matched || decision.OAuthAmbiguous() {
					return ctx, "", service.ErrNoAvailableAccounts
				}
				ctx = service.WithCompositeRouteDecision(ctx, decision)
				platform = decision.TargetPlatform
			}
			if !isResponsesWebSocketCompositePlatform(platform) {
				return ctx, "", service.ErrNoAvailableAccounts
			}
			forwardModel := model
			if upstream, ok := service.ResolvedUpstreamModelFromContext(ctx); ok {
				forwardModel = upstream
			}
			mapping, restricted := h.gatewayService.ResolveChannelMappingAndRestrict(ctx, candidate.GroupID, forwardModel)
			if restricted {
				return ctx, "", service.ErrNoAvailableAccounts
			}
			forwardModel = openAIChannelForwardModel(mapping, forwardModel)
			ctx, _ = h.gatewayService.WithOpenAIRequestPricingContext(ctx, candidate.GroupID)
			return ctx, forwardModel, nil
		}, func(ctx context.Context, candidate *service.APIKey, forwardModel string) (*service.AccountSelectionResult, service.OpenAIAccountScheduleDecision, error) {
			platform := candidate.Group.Platform
			if resolved, ok := service.ResolvedTargetPlatformFromContext(ctx); ok {
				platform = resolved
			}
			transport := service.OpenAIUpstreamTransportResponsesWebsocketV2Ingress
			if platform == service.PlatformGrok {
				transport = service.OpenAIUpstreamTransportHTTPSSE
			}
			capability := service.OpenAIEndpointCapabilityChatCompletions
			image := service.IsExplicitImageGenerationIntent("/v1/responses", model, body)
			if image && platform == service.PlatformOpenAI {
				capability = service.OpenAIEndpointCapabilityResponses
			}
			session := h.gatewayService.GenerateSessionHashWithFallback(c, body, openAIWSIngressFallbackSessionSeed(key.UserID, key.ID, candidate.GroupID))
			previous := strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String())
			coverage := service.AnalyzeToolCallOutputContextCoverageBytes(body)
			canMove := !coverage.HasFunctionCallOutput || coverage.ContextCoversAllCallIDs
			return h.gatewayService.SelectAccountWithSchedulerForCapability(ctx, candidate.GroupID, previous, session, forwardModel, nil, transport, capability, false, canMove, !image, platform)
		})
	if err != nil {
		return nil, nil, err
	}
	c.Request = c.Request.WithContext(ctx)
	middleware.BindAPIKeySmartRouteContext(c, selected)
	return selected, held, nil
}

type smartWSHydrate func(context.Context, *service.APIKey, int64) (*service.APIKey, error)
type smartWSPrepare func(context.Context, *service.APIKey) (context.Context, string, error)
type smartWSSelect func(context.Context, *service.APIKey, string) (*service.AccountSelectionResult, service.OpenAIAccountScheduleDecision, error)

func selectSmartWSRoute(ctx context.Context, key *service.APIKey, model string, body []byte, hydrate smartWSHydrate, prepare smartWSPrepare, selectOne smartWSSelect) (*service.APIKey, *smartWSRouteSelection, context.Context, error) {
	lastErr := service.ErrNoAvailableAccounts
	for _, id := range key.CandidateGroupIDs() {
		if err := ctx.Err(); err != nil {
			return nil, nil, ctx, err
		}
		candidate, err := hydrate(ctx, key, id)
		if err != nil {
			if errors.Is(err, service.ErrGroupNotFound) {
				continue
			}
			return nil, nil, ctx, err
		}
		if !smartWSRouteAllowed(candidate, requestmodel.FromBodyCandidates("", "application/json", body)) {
			continue
		}
		routeCtx := context.WithValue(ctx, ctxkey.Group, candidate.Group)
		routeCtx, forwardModel, err := prepare(routeCtx, candidate)
		if err != nil {
			if smartWSRouteUnavailable(err) {
				lastErr = err
				continue
			}
			return nil, nil, ctx, err
		}
		selection, decision, err := selectOne(routeCtx, candidate, forwardModel)
		if err != nil {
			if selection != nil && selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			if smartWSRouteUnavailable(err) {
				lastErr = err
				continue
			}
			return nil, nil, ctx, err
		}
		if selection == nil || selection.Account == nil {
			if selection != nil && selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			continue
		}
		return candidate, &smartWSRouteSelection{selection: selection, decision: decision}, routeCtx, nil
	}
	return nil, nil, ctx, lastErr
}
func smartWSRouteAllowed(key *service.APIKey, models []string) bool {
	return key != nil && key.Group != nil && key.Group.IsActive() && key.User != nil &&
		key.User.CanBindGroup(key.Group.ID, key.Group.IsExclusive) &&
		(isResponsesWebSocketCompositePlatform(key.Group.Platform) || key.Group.Platform == service.PlatformComposite) &&
		blockedModelAllowlistCandidate(key.Group, models) == ""
}
func smartWSRouteUnavailable(err error) bool {
	return errors.Is(err, service.ErrNoAvailableAccounts) || errors.Is(err, service.ErrNoAvailableCompactAccounts) || errors.Is(err, service.ErrClaudeCodeOnly)
}
