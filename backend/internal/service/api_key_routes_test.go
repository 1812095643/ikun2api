package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

type smartRouteGroupRepo struct {
	GroupRepository
	groups map[int64]*Group
}

func (r *smartRouteGroupRepo) GetByID(_ context.Context, id int64) (*Group, error) {
	if g := r.groups[id]; g != nil {
		return g, nil
	}
	return nil, ErrGroupNotFound
}
func (r *smartRouteGroupRepo) GetByIDLite(ctx context.Context, id int64) (*Group, error) {
	return r.GetByID(ctx, id)
}
func TestSmartRoutingNormalizationAndValidation(t *testing.T) {
	id := int64(4)
	ids, primary, err := normalizeAPIKeyGroupIDs(&id, []int64{4, 9, 2})
	require.NoError(t, err)
	require.Equal(t, []int64{4, 9, 2}, ids)
	require.Equal(t, int64(4), *primary)
	_, _, err = normalizeAPIKeyGroupIDs(&id, []int64{9, 4})
	require.Error(t, err)
	for _, bad := range [][]int64{{1, 1}, {1, 0}, {-1, 2}} {
		_, _, err = normalizeAPIKeyGroupIDs(nil, bad)
		require.Error(t, err)
	}
	groupRepo := &smartRouteGroupRepo{groups: map[int64]*Group{4: {ID: 4, Status: StatusActive}, 9: {ID: 9, Status: StatusActive, IsExclusive: true}}}
	svc := &APIKeyService{groupRepo: groupRepo}
	user := &User{ID: 7, AllowedGroups: []int64{9}}
	_, _, err = svc.validateAPIKeyGroupRoutes(context.Background(), user, nil, []int64{4, 9})
	require.NoError(t, err)
	user.AllowedGroups = nil
	_, _, err = svc.validateAPIKeyGroupRoutes(context.Background(), user, nil, []int64{4, 9})
	require.ErrorIs(t, err, ErrGroupNotAllowed)
	groupRepo.groups[4].Status = "disabled"
	_, _, err = svc.validateAPIKeyGroupRoutes(context.Background(), user, nil, []int64{4})
	require.ErrorIs(t, err, ErrGroupNotAllowed)
}
func TestSmartRoutingRequestBindingKeepsFundingAndCacheIdentity(t *testing.T) {
	primary := int64(1)
	owner := &User{ID: 7}
	rpm := 100
	owner.UserGroupRPMOverride = &rpm
	original := &APIKey{ID: 10, UserID: 7, GroupID: &primary, Group: &Group{ID: 1}, User: owner, FundingSource: "universal_subscription", RouteGroupIDs: []int64{1, 2}}
	svc := &APIKeyService{groupRepo: &smartRouteGroupRepo{groups: map[int64]*Group{2: {ID: 2, RateMultiplier: 3}}}}
	bound, err := svc.BindAPIKeyRequestGroup(context.Background(), original, 2)
	require.NoError(t, err)
	require.Equal(t, int64(2), *bound.GroupID)
	require.Equal(t, original.FundingSource, bound.FundingSource)
	require.Equal(t, int64(1), *original.GroupID)
	require.NotSame(t, owner, bound.User)
	require.Nil(t, bound.User.UserGroupRPMOverride)
	require.NotNil(t, owner.UserGroupRPMOverride)
	_, err = svc.BindAPIKeyRequestGroup(context.Background(), original, 3)
	require.ErrorIs(t, err, ErrGroupNotAllowed)
}
func TestSmartRoutingSchedulerOutcomeUsesFinalSelection(t *testing.T) {
	ctx, state := WithAPIKeyRouteSelection(context.Background())
	recordAPIKeyRouteSelection(ctx, ErrNoAvailableAccounts)
	require.True(t, state.Exhausted())
	recordAPIKeyRouteSelection(ctx, nil)
	require.False(t, state.Exhausted())
	recordAPIKeyRouteSelection(ctx, ErrSchedulerCacheNotReady)
	require.True(t, state.Exhausted())
	recordAPIKeyRouteSelection(ctx, context.Canceled)
	require.False(t, state.Exhausted())
}

func TestSmartRoutingMatchingProviderPassthroughPreservesNewModels(t *testing.T) {
	groupID := int64(8)
	repo := &modelsListAccountRepoStub{byGroup: map[int64][]Account{8: {{ID: 3, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Type: AccountTypeAPIKey, Credentials: map[string]any{}}}}}
	svc := &GatewayService{accountRepo: repo}
	key := &APIKey{GroupID: &groupID, Group: &Group{ID: 8, Platform: PlatformOpenAI}}
	require.True(t, svc.APIKeyRouteSupportsModel(context.Background(), key, "gpt-future-model"))
	require.False(t, svc.APIKeyRouteSupportsModel(context.Background(), key, "claude-future-model"))
}

type smartRouteRPMCounter struct {
	UserRPMCache
	userCalls, groupCalls int
}

func (c *smartRouteRPMCounter) IncrementUserRPM(context.Context, int64) (int, error) {
	c.userCalls++
	return c.userCalls, nil
}
func (c *smartRouteRPMCounter) IncrementUserGroupRPM(context.Context, int64, int64) (int, error) {
	c.groupCalls++
	return 1, nil
}
func TestSmartRoutingRPMCountsOneInboundRequestAcrossCandidateGroups(t *testing.T) {
	counter := &smartRouteRPMCounter{}
	svc := &BillingCacheService{userRPMCache: counter}
	user := &User{ID: 7, RPMLimit: 1}
	first, second := &Group{ID: 1, RPMLimit: 10}, &Group{ID: 2, RPMLimit: 10}
	ctx := WithAPIKeyRouteRPMReceipts(context.Background())
	require.NoError(t, svc.checkRPM(ctx, user, first))
	require.NoError(t, svc.checkRPM(ctx, user, second))
	require.NoError(t, svc.checkRPM(ctx, user, second))
	require.Equal(t, 1, counter.userCalls)
	require.Equal(t, 2, counter.groupCalls)
	nextRequest := WithAPIKeyRouteRPMReceipts(context.Background())
	require.ErrorIs(t, svc.checkRPM(nextRequest, user, first), ErrUserRPMExceeded)
	require.Equal(t, 2, counter.userCalls, "a fresh request must still enforce the global cap")
}

type smartRouteRPMOverrideRepo struct {
	UserGroupRateRepository
	calls int
}

func (r *smartRouteRPMOverrideRepo) GetRPMOverrideByUserAndGroup(context.Context, int64, int64) (*int, error) {
	r.calls++
	return nil, nil
}
func TestSmartRoutingReusesKnownNilRPMOverrideOnlyForItsGroup(t *testing.T) {
	primary := int64(1)
	lookup := &smartRouteRPMOverrideRepo{}
	owner := &User{ID: 7, UserGroupRPMOverrideGroupID: 1}
	original := &APIKey{ID: 10, UserID: 7, User: owner, GroupID: &primary, RouteGroupIDs: []int64{1, 2}}
	keys := &APIKeyService{userGroupRateRepo: lookup, groupRepo: &smartRouteGroupRepo{groups: map[int64]*Group{1: {ID: 1, RPMLimit: 10}, 2: {ID: 2, RPMLimit: 10}}}}
	first, err := keys.BindAPIKeyRequestGroup(context.Background(), original, 1)
	require.NoError(t, err)
	require.Zero(t, lookup.calls, "the auth snapshot already confirmed the primary group")
	second, err := keys.BindAPIKeyRequestGroup(context.Background(), original, 2)
	require.NoError(t, err)
	require.Equal(t, 1, lookup.calls, "a secondary group needs its own override lookup")
	require.Equal(t, int64(2), second.User.UserGroupRPMOverrideGroupID)
	billing := &BillingCacheService{userRPMCache: &smartRouteRPMCounter{}, userGroupRateRepo: lookup}
	ctx := WithAPIKeyRouteRPMReceipts(context.Background())
	require.NoError(t, billing.checkRPM(ctx, first.User, first.Group))
	require.NoError(t, billing.checkRPM(ctx, second.User, second.Group))
	require.Equal(t, 1, lookup.calls, "a confirmed nil override must not trigger another DB query")
	require.Equal(t, int64(1), owner.UserGroupRPMOverrideGroupID)
}
