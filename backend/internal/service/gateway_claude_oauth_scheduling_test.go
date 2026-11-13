//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestClaudeOAuthSchedulingScope(t *testing.T) {
	for _, tc := range []struct {
		platform, kind string
		enabled        bool
	}{
		{PlatformAnthropic, AccountTypeOAuth, true},
		{PlatformAnthropic, AccountTypeSetupToken, false},
		{PlatformAnthropic, AccountTypeAPIKey, false},
		{PlatformAnthropic, AccountTypeServiceAccount, false},
		{PlatformOpenAI, AccountTypeOAuth, false},
		{PlatformGemini, AccountTypeOAuth, false},
		{PlatformAntigravity, AccountTypeOAuth, false},
		{PlatformGrok, AccountTypeOAuth, false},
	} {
		t.Run(tc.platform+"/"+tc.kind, func(t *testing.T) {
			account := &Account{Platform: tc.platform, Type: tc.kind}
			require.Equal(t, tc.enabled, IsClaudeOAuthSchedulingAccount(account))
		})
	}
	require.False(t, IsClaudeOAuthSchedulingAccount(nil))
}

func TestFallbackQueue_ClaudeOAuthScopePreservesOtherSchedulers(t *testing.T) {
	for _, tc := range []struct {
		platform, kind string
		optimized      bool
	}{
		{PlatformAnthropic, AccountTypeOAuth, true},
		{PlatformAnthropic, AccountTypeSetupToken, false},
		{PlatformAnthropic, AccountTypeAPIKey, false},
		{PlatformOpenAI, AccountTypeOAuth, false},
		{PlatformGemini, AccountTypeOAuth, false},
		{PlatformGrok, AccountTypeOAuth, false},
	} {
		t.Run(tc.platform+"/"+tc.kind, func(t *testing.T) {
			accounts := []Account{fallbackQueueAccount(1, 1, 5), fallbackQueueAccount(2, 2, 5)}
			for i := range accounts {
				accounts[i].Platform = tc.platform
				accounts[i].Type = tc.kind
			}
			svc := newFallbackQueueTestService(accounts, map[int64]*AccountLoadInfo{
				1: {AccountID: 1, CurrentConcurrency: 5, WaitingCount: 9, LoadRate: 280},
				2: {AccountID: 2, CurrentConcurrency: 5, LoadRate: 100},
			}, true)
			ctx := context.WithValue(context.Background(), ctxkey.ForcePlatform, tc.platform)
			result, err := svc.SelectAccountWithLoadAwareness(ctx, nil, "", "", nil, "", 0)
			require.NoError(t, err)
			require.NotNil(t, result.WaitPlan)
			if tc.optimized {
				require.Equal(t, int64(2), result.Account.ID)
				require.True(t, result.WaitPlan.ClaudeOAuthScheduling)
				require.NotNil(t, result.WaitPlan.RevalidateAccount)
				require.Equal(t, []int64{1}, waitAlternateIDs(result.WaitPlan))
			} else {
				require.Equal(t, int64(1), result.Account.ID, "manual priority still wins outside Claude OAuth")
				require.False(t, result.WaitPlan.ClaudeOAuthScheduling)
				require.Nil(t, result.WaitPlan.RevalidateAccount)
				require.Empty(t, result.WaitPlan.Alternates)
			}
		})
	}
}

func TestFallbackQueue_DisabledKeepsPriorityInsteadOfPressure(t *testing.T) {
	svc := newFallbackQueueTestService([]Account{fallbackQueueAccount(1, 1, 5), fallbackQueueAccount(2, 2, 5)}, map[int64]*AccountLoadInfo{
		1: {AccountID: 1, CurrentConcurrency: 5, WaitingCount: 20, LoadRate: 500},
		2: {AccountID: 2, CurrentConcurrency: 5, LoadRate: 100},
	}, false)
	result, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, "", "", nil, "", 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Account.ID)
	require.Empty(t, result.WaitPlan.Alternates)
	require.False(t, result.WaitPlan.ClaudeOAuthScheduling)
}

func TestClaudeOAuthQueuePressureKeepsMixedPoolPositions(t *testing.T) {
	a := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	key := &Account{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}
	b := &Account{ID: 3, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	setup := &Account{ID: 4, Platform: PlatformAnthropic, Type: AccountTypeSetupToken}
	items := []*Account{a, key, b, setup}
	sortByQueuePressure(items, func(a *Account) *Account { return a }, map[int64]float64{1: 9, 3: 1})
	require.Equal(t, []*Account{b, key, a, setup}, items)
}

func TestClaudeOAuthSoftQuotaDoesNotFilterAPIKeyOrSetupToken(t *testing.T) {
	now := time.Now()
	reset := now.Add(time.Hour)
	mk := func(id int64, kind string, used float64) *Account {
		return &Account{ID: id, Platform: PlatformAnthropic, Type: kind, SessionWindowEnd: &reset, Extra: map[string]any{"session_window_utilization": used}}
	}
	oauthNear, keyNear, setupNear, oauthFresh := mk(1, AccountTypeOAuth, 0.9), mk(2, AccountTypeAPIKey, 0.99), mk(3, AccountTypeSetupToken, 0.99), mk(4, AccountTypeOAuth, 0.4)
	got := preferQuotaHeadroomAccounts([]*Account{oauthNear, keyNear, setupNear, oauthFresh}, 85, now)
	require.Equal(t, []*Account{keyNear, setupNear, oauthFresh}, got)
	for _, a := range []*Account{keyNear, setupNear} {
		require.Zero(t, accountQuotaUsedPercent(a, now))
	}
}

func TestClaudeOAuthQueuedAccountRevalidatesLatestState(t *testing.T) {
	model := "claude-sonnet-4-6"
	original := fallbackQueueAccount(1, 1, 5)
	for _, tc := range []struct {
		name   string
		mutate func(*Account)
	}{
		{"paused", func(a *Account) { a.Schedulable = false }},
		{"429 cooldown", func(a *Account) { until := time.Now().Add(time.Minute); a.RateLimitResetAt = &until }},
		{"model cooldown", func(a *Account) {
			a.Extra = map[string]any{"model_rate_limits": map[string]any{model: map[string]any{"rate_limit_reset_at": time.Now().Add(time.Minute).Format(time.RFC3339)}}}
		}},
		{"type changed", func(a *Account) { a.Type = AccountTypeAPIKey }},
		{"group changed", func(a *Account) { a.AccountGroups = []AccountGroup{{GroupID: 999}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			latest := original
			tc.mutate(&latest)
			repo := &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{1: &latest}}
			svc := &GatewayService{accountRepo: repo}
			got, err := svc.RevalidateWaitAlternate(context.Background(), &original, model)
			require.Error(t, err)
			require.Nil(t, got)
			require.Equal(t, 1, repo.getByIDCalls)
		})
	}
	svc := &GatewayService{accountRepo: &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{1: &original}}}
	got, err := svc.RevalidateWaitAlternate(context.Background(), &original, model)
	require.NoError(t, err)
	require.Same(t, &original, got)
}

func TestClaudeOAuthBindingChangesOnlyAfterSuccess(t *testing.T) {
	svc, ctx := newStickyBusyTestService(50_000)
	base := svc.cache.(*mockGatewayCacheForPlatform)
	selected, err := svc.SelectAccountWithLoadAwareness(ctx, nil, "sticky", "claude-sonnet-4-6", nil, "", 0)
	require.NoError(t, err)
	require.True(t, selected.Acquired)
	require.Equal(t, int64(1), base.sessionBindings["sticky"], "selection and a failed forward leave the old binding intact")
	require.NoError(t, svc.BindClaudeOAuthSessionAfterSuccess(ctx, nil, "sticky", selected.Account))
	require.Equal(t, selected.Account.ID, base.sessionBindings["sticky"])
	selected.ReleaseFunc()
	require.NoError(t, svc.BindClaudeOAuthSessionAfterSuccess(ctx, nil, "sticky", &Account{ID: 99, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}))
	require.Equal(t, selected.Account.ID, base.sessionBindings["sticky"], "API Key paths cannot use this rebinding")
}

func TestSlotWaitHub_CloseCannotBeReopenedByGraceTimer(t *testing.T) {
	hub := newSlotWaitHub()
	watcher := hub.watch()
	watcher.Add(SlotKindAccount, 1)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			watcher.Add(SlotKindAccount, 2)
		}
	}()
	go func() { defer wg.Done(); watcher.Close() }()
	wg.Wait()
	watcher.Add(SlotKindAccount, 3)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	require.Empty(t, hub.queues)
}

func TestClaudeOAuthWaitGraceLeavesTimeForAlternates(t *testing.T) {
	svc, ctx := newStickyBusyTestService(1000000)
	cfg := svc.schedulingConfig()
	plan := &AccountWaitPlan{AccountID: 1, Timeout: time.Second}
	ordered := []*Account{{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth}, {ID: 2, Platform: PlatformAnthropic, Type: AccountTypeOAuth}}
	svc.attachStickyWaitAlternates(ctx, cfg, plan, 4*time.Second, "sticky", func() []*Account { return ordered })
	require.Equal(t, 500*time.Millisecond, plan.AlternatesAfter)
	require.Equal(t, []int64{2}, waitAlternateIDs(plan))
}
