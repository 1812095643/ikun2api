//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestClaudeWeeklyResetOrder_ParsesOnlyValidOverallWeeklyWindows(t *testing.T) {
	now := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	end := now.Add(7 * time.Hour)
	order := newClaudeWeeklyResetOrder(&Group{Platform: PlatformAnthropic, PreferSoonestWeeklyReset: true}, now)
	for _, raw := range []any{end.Unix(), float64(end.Unix()), json.Number(strconv.FormatInt(end.Unix(), 10)), end.Format(time.RFC3339), end.UnixMilli()} {
		account := &Account{Platform: PlatformAnthropic, Extra: map[string]any{"passive_usage_7d_reset": raw, "passive_usage_7d_utilization": 0.97}}
		got, valid := order.resetAt(account)
		require.True(t, valid, "reset %v", raw)
		require.Equal(t, end, got.UTC())
	}
	for name, raw := range map[string]any{"missing": nil, "invalid": "bad", "zero": 0, "expired": now.Add(-time.Second).Unix(), "exact_reset": now.Unix(), "too_far": now.Add(8 * 24 * time.Hour).Unix(), "nan": math.NaN(), "infinity": math.Inf(1)} {
		t.Run(name, func(t *testing.T) {
			_, valid := order.resetAt(&Account{Platform: PlatformAnthropic, Extra: map[string]any{"passive_usage_7d_reset": raw}})
			require.False(t, valid)
		})
	}
	account := &Account{Platform: PlatformAnthropic, Extra: map[string]any{"passive_usage_7d_reset": end.Unix(), "passive_usage_7d_utilization": 1.0}}
	_, valid := order.resetAt(account)
	require.False(t, valid, "exhausted weekly windows must not be boosted")
	account.Platform = PlatformOpenAI
	_, valid = order.resetAt(account)
	require.False(t, valid, "GPT must not participate")
}

func TestClaudeWeeklyResetScheduling_AllSelectionPaths(t *testing.T) {
	for _, path := range []string{"load", "load_route", "load_batch_failure", "legacy", "legacy_route", "single", "single_route", "queue", "route_queue"} {
		t.Run(path, func(t *testing.T) {
			for _, enabled := range []bool{false, true} {
				now := time.Now()
				groupID := int64(10)
				group := &Group{ID: groupID, Platform: PlatformAnthropic, Status: StatusActive, Hydrated: true, PreferSoonestWeeklyReset: enabled}
				windows := []time.Duration{4 * 24 * time.Hour, 5 * 24 * time.Hour, 2 * 24 * time.Hour, 7 * time.Hour}
				repo := &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{}}
				for i, window := range windows {
					end5h := now.Add(time.Duration(i+1) * time.Hour)
					repo.accounts = append(repo.accounts, Account{ID: int64(i + 1), Type: AccountTypeOAuth, Platform: PlatformAnthropic, Status: StatusActive, Schedulable: true, Priority: i, Concurrency: 3, SessionWindowEnd: &end5h, AccountGroups: []AccountGroup{{GroupID: groupID}}, Extra: map[string]any{"passive_usage_7d_reset": now.Add(window).Unix(), "passive_usage_7d_utilization": 0.97}})
				}
				for i := range repo.accounts {
					repo.accountsByID[repo.accounts[i].ID] = &repo.accounts[i]
				}
				cache := &mockConcurrencyCache{}
				cfg := testConfig()
				cfg.Gateway.Scheduling.LoadBatchEnabled = true
				cfg.Gateway.Scheduling.PreferSoonestReset = true
				cfg.Gateway.Scheduling.FallbackSelectionMode = "random"
				if strings.Contains(path, "route") {
					group.ModelRoutingEnabled = true
					group.ModelRouting = map[string][]int64{"claude-sonnet-4-6": {1, 2, 3, 4}}
				}
				if path == "load_batch_failure" {
					cache.loadBatchErr = errors.New("load unavailable")
				}
				if path == "queue" || path == "route_queue" {
					cache.acquireResults = map[int64]bool{1: false, 2: false, 3: false, 4: false}
				}
				svc := &GatewayService{accountRepo: repo, groupRepo: &mockGroupRepoForGateway{groups: map[int64]*Group{groupID: group}}, cfg: cfg, concurrencyService: NewConcurrencyService(cache)}
				if path == "legacy" || path == "legacy_route" {
					svc.concurrencyService = nil
				}
				ctx := context.WithValue(context.Background(), ctxkey.Group, group)
				excluded := map[int64]struct{}{}
				want := []int64{1, 2, 3, 4}
				if enabled {
					want = []int64{4, 3, 1, 2}
				}
				for _, expected := range want {
					if path == "single" || path == "single_route" {
						account, err := svc.selectAccountForModelWithPlatform(ctx, &groupID, "", "claude-sonnet-4-6", excluded, PlatformAnthropic)
						require.NoError(t, err)
						require.Equal(t, expected, account.ID, "enabled=%v path=%s", enabled, path)
					} else {
						selected, err := svc.SelectAccountWithLoadAwareness(ctx, &groupID, "", "claude-sonnet-4-6", excluded, "", 0)
						require.NoError(t, err)
						require.Equal(t, expected, selected.Account.ID, "enabled=%v path=%s", enabled, path)
						if selected.ReleaseFunc != nil {
							selected.ReleaseFunc()
						}
						if path == "queue" || path == "route_queue" {
							require.NotNil(t, selected.WaitPlan)
						}
					}
					excluded[expected] = struct{}{}
				}
				for i := range repo.accounts {
					require.Equal(t, i, repo.accounts[i].Priority, "must not rewrite stored priorities")
				}
			}
		})
	}
}

func TestClaudeWeeklyResetScheduling_AdmissionAndSticky(t *testing.T) {
	for _, situation := range []string{"healthy", "429", "disabled", "overloaded", "temp_blocked", "full", "unknown", "expired", "busy", "acquire_race", "sticky"} {
		t.Run(situation, func(t *testing.T) {
			f := newLoadAwareRestrictionFixture(t, false, nil, nil)
			group := f.svc.groupRepo.(*mockGroupRepoForGateway).groups[f.groupID]
			group.PreferSoonestWeeklyReset = true
			repo := f.svc.accountRepo.(*mockAccountRepoForPlatform)
			now := time.Now()
			repo.accounts[0].Extra = map[string]any{"passive_usage_7d_reset": now.Add(4 * 24 * time.Hour).Unix()}
			repo.accounts[1].Extra = map[string]any{"passive_usage_7d_reset": now.Add(5 * time.Minute).Unix(), "passive_usage_7d_utilization": 0.78}
			later := now.Add(time.Hour)
			session := ""
			want := int64(1)
			switch situation {
			case "healthy":
				want = 2
			case "429":
				repo.accounts[1].RateLimitResetAt = &later
			case "disabled":
				repo.accounts[1].Schedulable = false
			case "overloaded":
				repo.accounts[1].OverloadUntil = &later
			case "temp_blocked":
				repo.accounts[1].TempUnschedulableUntil = &later
			case "full":
				repo.accounts[1].Extra["passive_usage_7d_utilization"] = 1.0
			case "unknown":
				delete(repo.accounts[1].Extra, "passive_usage_7d_reset")
			case "expired":
				repo.accounts[1].Extra["passive_usage_7d_reset"] = now.Add(-time.Minute).Unix()
			case "busy":
				f.concurrencyCache.loadMap = map[int64]*AccountLoadInfo{2: {AccountID: 2, CurrentConcurrency: 5, LoadRate: 100}}
			case "acquire_race":
				f.concurrencyCache.acquireResults = map[int64]bool{2: false}
			case "sticky":
				session = "existing"
				f.cache.sessionBindings = map[string]int64{session: 1}
				repo.accounts[0].AccountGroups = []AccountGroup{{GroupID: f.groupID}}
			}
			result, err := f.svc.SelectAccountWithLoadAwareness(f.ctx, &f.groupID, session, "claude-fable-5-1", nil, "", 0)
			require.NoError(t, err)
			require.Equal(t, want, result.Account.ID)
			if result.ReleaseFunc != nil {
				result.ReleaseFunc()
			}
		})
	}
}

func TestClaudeWeeklyResetOrder_EqualUnknownAndNonClaudeKeepOriginalOrder(t *testing.T) {
	now := time.Now()
	accounts := []*Account{{ID: 1, Platform: PlatformAnthropic, Extra: map[string]any{"passive_usage_7d_reset": now.Add(time.Hour).Unix()}}, {ID: 2, Platform: PlatformAnthropic, Extra: map[string]any{"passive_usage_7d_reset": now.Add(time.Hour).Unix()}}, {ID: 3, Platform: PlatformAnthropic}}
	order := newClaudeWeeklyResetOrder(&Group{Platform: PlatformAnthropic, PreferSoonestWeeklyReset: true}, now)
	order.sortAccounts(accounts)
	require.Equal(t, []int64{1, 2, 3}, []int64{accounts[0].ID, accounts[1].ID, accounts[2].ID})
	accounts[0], accounts[2] = accounts[2], accounts[0]
	for _, group := range []*Group{nil, {Platform: PlatformAnthropic}, {Platform: PlatformOpenAI, PreferSoonestWeeklyReset: true}} {
		newClaudeWeeklyResetOrder(group, now).sortAccounts(accounts)
		require.Equal(t, int64(3), accounts[0].ID)
	}
}

func TestClaudeWeeklyResetScheduling_DynamicUrgencyAndRollover(t *testing.T) {
	now := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	group := &Group{Platform: PlatformAnthropic, PreferSoonestWeeklyReset: true}
	near := &Account{ID: 1, Platform: PlatformAnthropic, Priority: 2_000_000_000, Extra: map[string]any{"passive_usage_7d_reset": now.Add(11 * time.Second).Unix()}}
	next := &Account{ID: 2, Platform: PlatformAnthropic, Priority: -2_000_000_000, Extra: map[string]any{"passive_usage_7d_reset": now.Add(12 * time.Second).Unix()}}
	order := newClaudeWeeklyResetOrder(group, now)
	require.Greater(t, order.score(near), order.score(next), "one second sooner must outweigh any manual priority")
	require.Equal(t, -1, order.compare(near, next))
	// The next request uses its current clock, rather than fixed time categories.
	order = newClaudeWeeklyResetOrder(group, now.Add(11*time.Second))
	require.Equal(t, 1, order.compare(near, next), "the expired window loses its urgency bonus")
	// A freshly sampled weekly reset starts a new window and is no longer urgent.
	near.Extra["passive_usage_7d_reset"] = now.Add(7 * 24 * time.Hour).Unix()
	require.Equal(t, 1, newClaudeWeeklyResetOrder(group, now).compare(near, next))
	group.PreferSoonestWeeklyReset = false
	require.Equal(t, 0, newClaudeWeeklyResetOrder(group, now).compare(near, next))
}
