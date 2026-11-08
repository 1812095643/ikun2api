package handler

import (
	"net/http"
	"strings"

	"github.com/MACOS-DO/sub4api/internal/pkg/httputil"
	"github.com/MACOS-DO/sub4api/internal/pkg/requestmodel"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
)

// SmartRouteMatchesRequest only reads local model catalogs, never probes an
// upstream. Actual group ACL, OAuth rules and account selection stay unchanged.
func (h *GatewayHandler) SmartRouteMatchesRequest(c *gin.Context, key *service.APIKey) (bool, error) {
	if key == nil || key.Group == nil {
		return false, nil
	}
	group := key.Group
	platform := group.Platform
	path := c.Request.URL.Path
	nativeGemini := strings.Contains(path, "/v1beta/")
	imageRequest := strings.Contains(path, "/images/") && !strings.Contains(path, "/tasks/")
	if nativeGemini && platform != service.PlatformGemini && platform != service.PlatformAntigravity && platform != service.PlatformComposite {
		return false, nil
	}
	if imageRequest && platform != service.PlatformOpenAI && platform != service.PlatformGrok && platform != service.PlatformComposite {
		return false, nil
	}
	if strings.Contains(path, "/videos") && platform != service.PlatformGrok && platform != service.PlatformComposite {
		return false, nil
	}
	if strings.Contains(path, "/embeddings") && platform != service.PlatformOpenAI && platform != service.PlatformComposite {
		return false, nil
	}
	if strings.HasSuffix(path, "/responses") && c.Request.Method == http.MethodGet && platform != service.PlatformOpenAI && platform != service.PlatformGrok && platform != service.PlatformComposite {
		return false, nil
	}
	if strings.HasSuffix(path, "/messages") && platform == service.PlatformOpenAI && !group.AllowMessagesDispatch {
		return false, nil
	}
	// Async video polling has no model. Resolve only this authenticated
	// key/user's server-owned task binding; never probe arbitrary upstream IDs.
	if c.Request.Method == http.MethodGet && strings.Contains(path, "/videos/") && c.Param("request_id") != "" {
		if h == nil || h.openAIGatewayService == nil {
			return false, service.ErrSchedulerCacheNotReady
		}
		accountID, err := h.openAIGatewayService.ResolveGrokMediaVideoRequestAccount(c.Request.Context(), key.GroupID, c.Param("request_id"), key.UserID, key.ID)
		if err != nil {
			return false, service.ErrSchedulerCacheNotReady
		}
		return accountID > 0, nil
	}
	model := strings.TrimSpace(c.Param("model"))
	if action := strings.TrimPrefix(c.Param("modelAction"), "/"); action != "" {
		model = strings.SplitN(action, ":", 2)[0]
	}
	if model == "" {
		model = strings.TrimSpace(c.Query("model"))
	}
	if model == "" && c.Request.Method != http.MethodGet && c.Request.Method != http.MethodDelete {
		body, err := httputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			return false, err
		}
		requestmodel.ResetRequestBody(c.Request, body)
		parsed := body
		if !strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "multipart/") {
			if normalized, err := httputil.NormalizeLenientJSONRequestBody(body, int64(len(body))); err == nil {
				parsed = normalized
			}
		}
		model = requestmodel.FromBodyForRoute(c.FullPath(), c.GetHeader("Content-Type"), parsed)
	}
	// Multipart image generation may omit the model; only routes whose group
	// permits images are eligible even when discovery has no model to inspect.
	if imageRequest && !service.GroupAllowsImageGeneration(group) {
		return false, nil
	}
	if model == "" {
		return true, nil
	}
	if group.ModelAllowlistEnabled() && !group.ModelAllowlist.Allows(model) {
		return false, nil
	}
	if platform == service.PlatformComposite {
		return true, nil
	}
	if h == nil || h.gatewayService == nil {
		return false, service.ErrSchedulerCacheNotReady
	}
	if h.gatewayService.APIKeyRouteSupportsModel(c.Request.Context(), key, model) {
		return true, nil
	}
	if strings.Contains(path, "/messages") {
		if mapped := group.ResolveMessagesDispatchModel(model); mapped != "" {
			return h.gatewayService.APIKeyRouteSupportsModel(c.Request.Context(), key, mapped), nil
		}
	}
	return false, nil
}
