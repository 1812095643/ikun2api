//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

// newFallbackQueueTestService 构造所有账号都抢不到槽位、负载按 loads 返回的调度服务，
// 用于直接落到 Layer 3 兜底排队。
func newFallbackQueueTestService(accounts []Account, loads map[int64]*AccountLoadInfo, waitAny bool) *GatewayService {
	repo := &mockAccountRepoForPlatform{accounts: accounts, accountsByID: map[int64]*Account{}}
	acquire := map[int64]bool{}
	for i := range repo.accounts {
		repo.accountsByID[repo.accounts[i].ID] = &repo.accounts[i]
		acquire[repo.accounts[i].ID] = false
	}
	cfg := testConfig()
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	cfg.Gateway.Scheduling.FallbackWaitAnyAccount = waitAny
	return &GatewayService{
		accountRepo: repo,
		cache:       &mockGatewayCacheForPlatform{},
		cfg:         cfg,
		concurrencyService: NewConcurrencyService(&mockConcurrencyCache{
			acquireResults:  acquire,
			loadMap:         loads,
			skipDefaultLoad: true,
		}),
	}
}

func fallbackQueueAccount(id int64, priority, concurrency int) Account {
	return Account{ID: id, Platform: PlatformAnthropic, Priority: priority, Status: StatusActive, Schedulable: true, Concurrency: concurrency}
}

func waitAlternateIDs(plan *AccountWaitPlan) []int64 {
	ids := make([]int64, 0, len(plan.Alternates))
	for _, acc := range plan.Alternates {
		ids = append(ids, acc.ID)
	}
	return ids
}

func TestFallbackQueue_WaitsOnLeastPressuredAccount(t *testing.T) {
	svc := newFallbackQueueTestService(
		[]Account{fallbackQueueAccount(1, 1, 5), fallbackQueueAccount(2, 1, 5), fallbackQueueAccount(3, 1, 5)},
		map[int64]*AccountLoadInfo{
			1: {AccountID: 1, CurrentConcurrency: 5, WaitingCount: 10, LoadRate: 300},
			2: {AccountID: 2, CurrentConcurrency: 5, WaitingCount: 2, LoadRate: 140},
			3: {AccountID: 3, CurrentConcurrency: 5, WaitingCount: 6, LoadRate: 220},
		},
		true,
	)

	result, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, "", "claude-3-5-sonnet-20241022", nil, "", 0)
	require.NoError(t, err)
	require.NotNil(t, result.WaitPlan)
	require.Equal(t, int64(2), result.Account.ID)
	require.Equal(t, int64(2), result.WaitPlan.AccountID)
	require.Equal(t, []int64{3, 1}, waitAlternateIDs(result.WaitPlan), "alternates follow queue pressure")
}

func TestFallbackQueue_PressureUsesLoadFactor(t *testing.T) {
	// 账号 1：10 并发跑满、无人排队 → 压力 1.0；账号 2：2 并发跑满、1 人排队 → 压力 1.5。
	svc := newFallbackQueueTestService(
		[]Account{fallbackQueueAccount(2, 1, 2), fallbackQueueAccount(1, 1, 10)},
		map[int64]*AccountLoadInfo{
			1: {AccountID: 1, CurrentConcurrency: 10},
			2: {AccountID: 2, CurrentConcurrency: 2, WaitingCount: 1},
		},
		true,
	)

	result, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, "", "claude-3-5-sonnet-20241022", nil, "", 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.WaitPlan.AccountID)
}

func TestFallbackQueue_EqualPressureKeepsPriorityOrder(t *testing.T) {
	svc := newFallbackQueueTestService(
		[]Account{fallbackQueueAccount(1, 2, 5), fallbackQueueAccount(2, 1, 5)},
		map[int64]*AccountLoadInfo{
			1: {AccountID: 1, CurrentConcurrency: 5, WaitingCount: 1},
			2: {AccountID: 2, CurrentConcurrency: 5, WaitingCount: 1},
		},
		true,
	)

	result, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, "", "claude-3-5-sonnet-20241022", nil, "", 0)
	require.NoError(t, err)
	require.Equal(t, int64(2), result.WaitPlan.AccountID, "lower priority value wins a pressure tie")
	require.Equal(t, []int64{1}, waitAlternateIDs(result.WaitPlan))
}

func TestFallbackQueue_AlternatesDisabledByConfig(t *testing.T) {
	svc := newFallbackQueueTestService(
		[]Account{fallbackQueueAccount(1, 1, 5), fallbackQueueAccount(2, 1, 5)},
		map[int64]*AccountLoadInfo{
			1: {AccountID: 1, CurrentConcurrency: 5},
			2: {AccountID: 2, CurrentConcurrency: 5},
		},
		false,
	)

	result, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, "", "claude-3-5-sonnet-20241022", nil, "", 0)
	require.NoError(t, err)
	require.NotNil(t, result.WaitPlan)
	require.Empty(t, result.WaitPlan.Alternates)
}

func TestFallbackWaitAlternates_SkipsSessionLimitedAccounts(t *testing.T) {
	svc := &GatewayService{sessionLimitCache: newSessionLimitReleaseCacheStub()}
	cfg := testConfig().Gateway.Scheduling
	cfg.FallbackWaitAnyAccount = true

	limited := &Account{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{"max_sessions": 3}}
	unlimitedOAuth := &Account{ID: 3, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	apiKey := &Account{ID: 4, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Extra: map[string]any{"max_sessions": 3}}
	chosen := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	ordered := []*Account{chosen, limited, unlimitedOAuth, apiKey}

	got := svc.fallbackWaitAlternates(cfg, chosen.ID, ordered, "session-1")
	require.Equal(t, []int64{3, 4}, waitAlternateIDs(&AccountWaitPlan{Alternates: got}))

	// 无会话 ID 时会话配额不生效，受限账号也可替换。
	got = svc.fallbackWaitAlternates(cfg, chosen.ID, ordered, "")
	require.Equal(t, []int64{2, 3, 4}, waitAlternateIDs(&AccountWaitPlan{Alternates: got}))
}

func TestFallbackWaitAlternates_Capped(t *testing.T) {
	svc := &GatewayService{}
	cfg := testConfig().Gateway.Scheduling
	cfg.FallbackWaitAnyAccount = true
	ordered := make([]*Account, 0, 20)
	for i := int64(1); i <= 20; i++ {
		ordered = append(ordered, &Account{ID: i, Platform: PlatformAnthropic, Type: AccountTypeAPIKey})
	}
	require.Len(t, svc.fallbackWaitAlternates(cfg, 1, ordered, ""), maxFallbackWaitAlternates)
}

func TestFreshQueuePressure_LoadErrorKeepsOrder(t *testing.T) {
	accounts := []*Account{{ID: 1, Concurrency: 5}, {ID: 2, Concurrency: 5}}
	pressure := freshQueuePressure(context.Background(), NewConcurrencyService(&mockConcurrencyCache{loadBatchErr: errors.New("redis down")}), accounts)
	require.Nil(t, pressure)

	sortByQueuePressure(accounts, func(acc *Account) *Account { return acc }, pressure)
	require.Equal(t, int64(1), accounts[0].ID)
	require.Equal(t, int64(2), accounts[1].ID)
}

// 复现分组 19：开启「优先周额度最早重置」时，原实现把整波溢出请求排到重置最早的账号；
// 现在它排队最长时应让位给排队最短的账号，重置顺序只在压力相同时生效。
func TestFallbackQueue_WeeklyResetPreferenceYieldsToQueuePressure(t *testing.T) {
	now := time.Now()
	groupID := int64(19)
	group := &Group{ID: groupID, Platform: PlatformAnthropic, Status: StatusActive, Hydrated: true, PreferSoonestWeeklyReset: true}
	repo := &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{}}
	resets := map[int64]time.Duration{29: 24 * time.Hour, 26: 2 * 24 * time.Hour, 30: 3 * 24 * time.Hour}
	for _, id := range []int64{29, 26, 30} {
		repo.accounts = append(repo.accounts, Account{ID: id, Type: AccountTypeOAuth, Platform: PlatformAnthropic, Status: StatusActive, Schedulable: true, Priority: 1, Concurrency: 5, AccountGroups: []AccountGroup{{GroupID: groupID}}, Extra: map[string]any{"passive_usage_7d_reset": now.Add(resets[id]).Unix(), "passive_usage_7d_utilization": 0.5}})
	}
	for i := range repo.accounts {
		repo.accountsByID[repo.accounts[i].ID] = &repo.accounts[i]
	}
	cfg := testConfig()
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	cfg.Gateway.Scheduling.FallbackWaitAnyAccount = true
	cache := &mockConcurrencyCache{
		acquireResults:  map[int64]bool{29: false, 26: false, 30: false},
		skipDefaultLoad: true,
		loadMap: map[int64]*AccountLoadInfo{
			29: {AccountID: 29, CurrentConcurrency: 5, WaitingCount: 14},
			26: {AccountID: 26, CurrentConcurrency: 5, WaitingCount: 1},
			30: {AccountID: 30, CurrentConcurrency: 5, WaitingCount: 1},
		},
	}
	svc := &GatewayService{accountRepo: repo, groupRepo: &mockGroupRepoForGateway{groups: map[int64]*Group{groupID: group}}, cfg: cfg, concurrencyService: NewConcurrencyService(cache)}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)

	result, err := svc.SelectAccountWithLoadAwareness(ctx, &groupID, "", "claude-sonnet-4-6", nil, "", 0)
	require.NoError(t, err)
	require.NotNil(t, result.WaitPlan)
	require.Equal(t, int64(26), result.WaitPlan.AccountID, "26 resets sooner than 30 and ties on pressure")
	require.Equal(t, []int64{30, 29}, waitAlternateIDs(result.WaitPlan))
}

func newStickyBusyTestService(hint int) (*GatewayService, context.Context) {
	repo := &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{}}
	for _, id := range []int64{1, 2, 3} {
		repo.accounts = append(repo.accounts, Account{ID: id, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Priority: 1, Status: StatusActive, Schedulable: true, Concurrency: 5})
	}
	for i := range repo.accounts {
		repo.accountsByID[repo.accounts[i].ID] = &repo.accounts[i]
	}
	cfg := testConfig()
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 3
	cfg.Gateway.Scheduling.FallbackWaitAnyAccount = true
	cfg.Gateway.Scheduling.StickyCacheGrace = 2 * time.Second
	cfg.Gateway.Scheduling.StickyCacheGraceLarge = 5 * time.Second
	cfg.Gateway.Scheduling.StickyCacheGraceMinBytes = 200_000
	cfg.Gateway.Scheduling.StickyCacheGraceLargeBytes = 800_000
	svc := &GatewayService{
		accountRepo: repo,
		cache:       &mockGatewayCacheForPlatform{sessionBindings: map[string]int64{"sticky": 1}},
		cfg:         cfg,
		concurrencyService: NewConcurrencyService(&mockConcurrencyCache{
			acquireResults:  map[int64]bool{1: false, 2: true, 3: true},
			waitCounts:      map[int64]int{1: 0},
			skipDefaultLoad: true,
			loadMap: map[int64]*AccountLoadInfo{
				1: {AccountID: 1, CurrentConcurrency: 5},
				2: {AccountID: 2, CurrentConcurrency: 4},
				3: {AccountID: 3, CurrentConcurrency: 1},
			},
		}),
	}
	return svc, WithPromptSizeHint(context.Background(), hint)
}

func TestStickyWait_AlternatesWithCacheGraceBySize(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bytes int
		grace time.Duration
	}{
		{"unknown size uses normal grace", 0, 2 * time.Second},
		{"small request switches immediately", 50_000, 0},
		{"medium request waits normal grace", 300_000, 2 * time.Second},
		{"large request waits longer", 1_000_000, 5 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, ctx := newStickyBusyTestService(tc.bytes)
			result, err := svc.SelectAccountWithLoadAwareness(ctx, nil, "sticky", "claude-3-5-sonnet-20241022", nil, "", 0)
			require.NoError(t, err)
			require.NotNil(t, result.WaitPlan, "busy sticky account keeps its wait plan")
			require.Equal(t, int64(1), result.WaitPlan.AccountID)
			require.Equal(t, cfgStickyTimeout(svc), result.WaitPlan.Timeout)
			require.Equal(t, []int64{3, 2}, waitAlternateIDs(result.WaitPlan), "alternates ordered by queue pressure")
			require.Equal(t, tc.grace, result.WaitPlan.AlternatesAfter)
		})
	}
}

func TestStickyWait_NoAlternatesWhenDisabled(t *testing.T) {
	svc, ctx := newStickyBusyTestService(300_000)
	svc.cfg.Gateway.Scheduling.FallbackWaitAnyAccount = false
	result, err := svc.SelectAccountWithLoadAwareness(ctx, nil, "sticky", "claude-3-5-sonnet-20241022", nil, "", 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.WaitPlan.AccountID)
	require.Empty(t, result.WaitPlan.Alternates)
	require.Zero(t, result.WaitPlan.AlternatesAfter)
}

func cfgStickyTimeout(svc *GatewayService) time.Duration {
	return svc.cfg.Gateway.Scheduling.StickySessionWaitTimeout
}

func TestApplyGatewayTiming(t *testing.T) {
	upstream := 1800
	log := &UsageLog{FirstTokenMs: &upstream}
	applyGatewayTiming(log, &GatewayRequestTiming{SlotWait: 3 * time.Second, PreForward: 3500 * time.Millisecond, SwitchCount: 2})
	require.Equal(t, 3000, *log.GatewayWaitMs)
	require.Equal(t, 3500, *log.PreForwardMs)
	require.Equal(t, 2, *log.AccountSwitchCount)
	require.Equal(t, 5300, *log.ClientFirstTokenMs)

	nonStream := &UsageLog{}
	applyGatewayTiming(nonStream, &GatewayRequestTiming{PreForward: time.Second})
	require.Nil(t, nonStream.ClientFirstTokenMs, "no upstream first token, no client first token")
	require.Equal(t, 1000, *nonStream.PreForwardMs)

	untouched := &UsageLog{FirstTokenMs: &upstream}
	applyGatewayTiming(untouched, nil)
	require.Nil(t, untouched.PreForwardMs)
}
