package service

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type poolTestAdmin struct {
	AdminService
	accounts []*Account
}

func (a poolTestAdmin) GetAccountsByIDs(context.Context, []int64) ([]*Account, error) {
	return a.accounts, nil
}

type poolTestUsageRepo struct {
	UsageLogRepository
	users  []int64
	groups []int64
}

func (r *poolTestUsageRepo) ListAccountIDsByUser(_ context.Context, userID int64, _ time.Time, groupIDs []int64) ([]int64, error) {
	r.users = append(r.users, userID)
	r.groups = append(r.groups, groupIDs...)
	return []int64{9}, nil
}

func (r *poolTestUsageRepo) GetUserAccountWindowStatsBatch(_ context.Context, userID int64, _ []int64, _ time.Time, groupIDs []int64) (map[int64]*usagestats.AccountStats, error) {
	r.users = append(r.users, userID)
	r.groups = append(r.groups, groupIDs...)
	return map[int64]*usagestats.AccountStats{9: {Requests: 3, Tokens: 20, Cost: 900, UserCost: 2, StandardCost: 4}}, nil
}

func TestAccountPoolUserScopeAndMaskedSearch(t *testing.T) {
	account := &Account{ID: 9, Name: "private-account@example.test", Platform: "anthropic", Type: "oauth", Status: StatusActive}
	repo := &poolTestUsageRepo{}
	svc := NewAccountPoolService(poolTestAdmin{accounts: []*Account{account}}, nil, nil, repo, nil)
	result, err := svc.ListAccountsForUser(context.Background(), 42, PoolAccountFilter{})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, MaskAccountName(account.Name), result.Items[0].Name)
	require.Equal(t, float64(2), result.Items[0].Today.Cost)
	require.Equal(t, float64(2), result.Items[0].Today.UserCost)
	for _, id := range repo.users {
		require.Equal(t, int64(42), id)
	}
	payload, err := json.Marshal(result.Items)
	require.NoError(t, err)
	require.NotContains(t, string(payload), account.Name)
	require.NotContains(t, string(payload), "rate_multiplier")
	require.NotContains(t, string(payload), "credentials")

	// A hidden part of the raw name must not be discoverable with a search oracle.
	result, err = svc.ListAccountsForUser(context.Background(), 42, PoolAccountFilter{Search: "vate-account"})
	require.NoError(t, err)
	require.Empty(t, result.Items)

	result, err = svc.ListAccountsForUser(context.Background(), 42, PoolAccountFilter{Page: math.MaxInt, PageSize: 200})
	require.NoError(t, err)
	require.Empty(t, result.Items)
	require.Equal(t, int64(1), result.Total)
}

func TestAccountPoolNameMasking(t *testing.T) {
	for _, name := range []string{"private-account@example.test", "upstream-provider-123", "我的专属账号名称", "ab", "a"} {
		t.Run(name, func(t *testing.T) {
			masked := MaskAccountName(name)
			require.NotEqual(t, name, masked)
			require.Contains(t, masked, "***")
			require.NotContains(t, masked, "****")
			require.Equal(t, masked, PoolMaskAccountName(name))
			for _, kind := range []string{"oauth", "apikey"} {
				require.Equal(t, masked, poolDisplayAccountNameForUser(&Account{ID: 9, Name: name, Type: kind}, 42, time.Now()))
			}
		})
	}
	name := "before-rename@example.test"
	copy := MaskAccountNameSnapshot(&name)
	require.NotNil(t, copy)
	require.Equal(t, "before-rename@example.test", name)
	require.Nil(t, MaskAccountNameSnapshot(nil))
	blank := " "
	require.Nil(t, MaskAccountNameSnapshot(&blank))
}

func TestAccountPoolQuotaReadings(t *testing.T) {
	for _, test := range []struct {
		name  string
		raw   any
		want  float64
		valid bool
	}{
		{"zero", float64(0), 0, true},
		{"fraction", 0.62, 62, true},
		{"percent", float64(62), 62, true},
		{"nan", math.NaN(), 0, false},
		{"infinity", math.Inf(1), 0, false},
		{"negative", -0.1, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, ok := poolExtraPercent(map[string]any{"reading": test.raw}, "reading")
			require.Equal(t, test.valid, ok)
			require.Equal(t, test.want, value)
		})
	}
	past := time.Now().Add(-time.Hour)
	account := &Account{
		SessionWindowEnd: &past,
		Extra: map[string]any{
			"session_window_utilization":   0.9,
			"passive_usage_7d_utilization": 0.9,
			"passive_usage_7d_reset":       past.Format(time.RFC3339),
		},
	}
	for _, kind := range []string{poolWindowFiveHour, poolWindowSevenDay} {
		window := poolQuotaWindowFromAccount(account, kind, PoolCapacity{})
		require.Zero(t, window.Utilization)
		require.Nil(t, window.ResetsAt)
		require.Zero(t, window.RemainingSeconds)
	}
}
