package service

// Ordered key routing adapted from kiss-kedaya/sub2api, revision c33708f.
import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	infraerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
)

func (k *APIKey) CandidateGroupIDs() []int64 {
	if k == nil {
		return nil
	}
	if len(k.RouteGroupIDs) > 0 {
		out := make([]int64, 0, len(k.RouteGroupIDs))
		seen := make(map[int64]struct{}, len(k.RouteGroupIDs))
		for _, id := range k.RouteGroupIDs {
			if id <= 0 {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
		if len(out) > 0 {
			return out
		}
	}
	if k.GroupID != nil && *k.GroupID > 0 {
		return []int64{*k.GroupID}
	}
	return nil
}

// UsesRequestTargetPlatform reports whether this key should dispatch and
// schedule from the requested model rather than the primary group's platform.
// Composite groups already do this; smart-routing keys with more than one
// bound group need the same treatment so a later OpenAI group is reachable
// when the primary group is Claude (and the reverse).

func normalizeAPIKeyGroupIDs(groupID *int64, groupIDs []int64) ([]int64, *int64, error) {
	cleaned := make([]int64, 0, len(groupIDs))
	seen := make(map[int64]struct{}, len(groupIDs))
	for _, id := range groupIDs {
		if id <= 0 {
			return nil, nil, infraerrors.BadRequest("API_KEY_GROUP_IDS_INVALID", "group_ids must contain positive IDs")
		}
		if _, ok := seen[id]; ok {
			return nil, nil, infraerrors.BadRequest("API_KEY_GROUP_IDS_DUPLICATE", "group_ids must not contain duplicates")
		}
		seen[id] = struct{}{}
		cleaned = append(cleaned, id)
	}
	if len(cleaned) == 0 {
		if groupID != nil && *groupID > 0 {
			return []int64{*groupID}, groupID, nil
		}
		return nil, groupID, nil
	}
	if groupID != nil && *groupID > 0 && *groupID != cleaned[0] {
		return nil, nil, infraerrors.BadRequest("API_KEY_GROUP_ID_MISMATCH", "group_id must match the first group_ids entry")
	}
	first := cleaned[0]
	return cleaned, &first, nil
}

func cloneAPIKeyWithGroupID(apiKey *APIKey, group *Group) *APIKey {
	if apiKey == nil {
		return nil
	}
	cloned := *apiKey
	if group != nil {
		id := group.ID
		cloned.GroupID = &id
		cloned.Group = group
	}
	return &cloned
}

func persistedRouteGroupIDs(routeIDs []int64) []int64 {
	if len(routeIDs) <= 1 {
		return nil
	}
	out := make([]int64, len(routeIDs))
	copy(out, routeIDs)
	return out
}

func (s *APIKeyService) validateAPIKeyGroupRoutes(ctx context.Context, user *User, groupID *int64, groupIDs []int64) ([]int64, *int64, error) {
	routeIDs, primary, err := normalizeAPIKeyGroupIDs(groupID, groupIDs)
	if err != nil {
		return nil, nil, err
	}
	if len(routeIDs) == 0 {
		return nil, primary, nil
	}
	for _, id := range routeIDs {
		group, err := s.groupRepo.GetByID(ctx, id)
		if err != nil {
			return nil, nil, fmt.Errorf("get group: %w", err)
		}
		if !group.IsActive() || !s.canUserBindGroup(ctx, user, group) {
			return nil, nil, ErrGroupNotAllowed
		}
	}
	return routeIDs, primary, nil
}

// BindAPIKeyRequestGroup hydrates a server-selected candidate without modifying
// the auth-cache object. Every inner request still runs the normal ACL, funding,
// quota, model and client checks before account scheduling.
func (s *APIKeyService) BindAPIKeyRequestGroup(ctx context.Context, key *APIKey, groupID int64) (*APIKey, error) {
	if key == nil {
		return nil, ErrAPIKeyNotFound
	}
	allowed := false
	for _, id := range key.CandidateGroupIDs() {
		if id == groupID {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, ErrGroupNotAllowed
	}
	group, err := s.groupRepo.GetByIDLite(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if group == nil {
		return nil, ErrGroupNotFound
	}
	cloned := cloneAPIKeyWithGroupID(key, group)
	if key.User != nil {
		user := *key.User
		if user.UserGroupRPMOverrideGroupID != groupID {
			user.UserGroupRPMOverride = nil
			user.UserGroupRPMOverrideGroupID = 0
			if s.userGroupRateRepo != nil {
				override, err := s.userGroupRateRepo.GetRPMOverrideByUserAndGroup(ctx, key.UserID, groupID)
				if err != nil {
					return nil, err
				}
				user.UserGroupRPMOverride = override
				user.UserGroupRPMOverrideGroupID = groupID
			}
		}
		cloned.User = &user
	}
	return cloned, nil
}

// Use the same available-model cache and channel aliases as /models. A miss
// skips the group instead of sending a foreign protocol to an OAuth account.
func (s *GatewayService) APIKeyRouteSupportsModel(ctx context.Context, key *APIKey, model string) bool {
	if s == nil || key == nil || key.GroupID == nil || key.Group == nil {
		return false
	}
	if strings.TrimSpace(model) == "" {
		return true
	}
	candidates := []string{model, normalizeRequestedModelForLookup(key.Group.Platform, model), NormalizeOpenAICompatRequestedModel(model)}
	mapping, restricted := s.ResolveChannelMappingAndRestrict(ctx, key.GroupID, model)
	if restricted {
		return false
	}
	if mapping.Mapped && mapping.MappedModel != "" {
		candidates = append(candidates, mapping.MappedModel)
	}
	models := s.GetAvailableModels(ctx, key.GroupID, "")
	for _, candidate := range candidates {
		for _, id := range models {
			if strings.EqualFold(id, candidate) || matchWildcard(id, candidate) {
				return true
			}
		}
	}
	// A new model may be absent from the cached public catalog while an
	// unmapped/passthrough account legitimately supports it. Keep this fallback
	// within a recognized matching provider and reuse account admission rules.
	detected, known := DetectModelPlatform(model)
	if known && detected == key.Group.Platform && !key.Group.ModelAllowlistEnabled() && s.accountRepo != nil {
		accounts, err := s.accountRepo.ListSchedulableByGroupID(ctx, *key.GroupID)
		if err == nil {
			for i := range accounts {
				if accounts[i].Platform == detected && accounts[i].IsModelSupported(model) {
					return true
				}
			}
		}
	}
	return false
}

// A scheduler-owned outcome distinguishes exhausted account groups from an
// arbitrary upstream error. Only the final selection result controls fallback.
type APIKeyRouteSelection struct{ exhausted atomic.Bool }
type apiKeyRouteSelectionKey struct{}

func WithAPIKeyRouteSelection(ctx context.Context) (context.Context, *APIKeyRouteSelection) {
	state := &APIKeyRouteSelection{}
	return context.WithValue(ctx, apiKeyRouteSelectionKey{}, state), state
}
func (s *APIKeyRouteSelection) Exhausted() bool { return s != nil && s.exhausted.Load() }
func recordAPIKeyRouteSelection(ctx context.Context, err error) {
	if ctx == nil {
		return
	}
	state, _ := ctx.Value(apiKeyRouteSelectionKey{}).(*APIKeyRouteSelection)
	if state == nil {
		return
	}
	state.exhausted.Store(errors.Is(err, ErrNoAvailableAccounts) || errors.Is(err, ErrSchedulerCacheNotReady) || errors.Is(err, ErrClaudeCodeOnly) || errors.Is(err, ErrNoAvailableCompactAccounts))
}

// Candidate attempts belong to one inbound HTTP request. Keep the user's
// global RPM count once, while applying each selected group's own limit.
// WebSocket connections do not use this receipt: their turns remain separate.
type apiKeyRouteRPMKey struct{}
type apiKeyRouteRPMScope struct {
	userID, groupID int64
	global          bool
}
type apiKeyRouteRPMReceipts struct {
	mu      sync.Mutex
	results map[apiKeyRouteRPMScope]error
}

func WithAPIKeyRouteRPMReceipts(ctx context.Context) context.Context {
	return context.WithValue(ctx, apiKeyRouteRPMKey{}, &apiKeyRouteRPMReceipts{results: make(map[apiKeyRouteRPMScope]error)})
}
func (r *apiKeyRouteRPMReceipts) once(scope apiKeyRouteRPMScope, check func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err, done := r.results[scope]; done {
		return err
	}
	err := check()
	r.results[scope] = err
	return err
}
func (s *BillingCacheService) checkSmartRouteRPM(ctx context.Context, user *User, group *Group) (error, bool) {
	if ctx == nil {
		return nil, false
	}
	receipts, _ := ctx.Value(apiKeyRouteRPMKey{}).(*apiKeyRouteRPMReceipts)
	if receipts == nil {
		return nil, false
	}
	inner := context.WithValue(ctx, apiKeyRouteRPMKey{}, (*apiKeyRouteRPMReceipts)(nil))
	if group != nil {
		groupUser := *user
		groupUser.RPMLimit = 0
		err := receipts.once(apiKeyRouteRPMScope{userID: user.ID, groupID: group.ID}, func() error { return s.checkRPM(inner, &groupUser, group) })
		if err != nil {
			return err, true
		}
	}
	err := receipts.once(apiKeyRouteRPMScope{userID: user.ID, global: true}, func() error { return s.checkRPM(inner, user, nil) })
	return err, true
}
