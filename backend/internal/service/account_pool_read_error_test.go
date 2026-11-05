package service

import (
	"context"
	"testing"
	"time"

	infraerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
	"github.com/MACOS-DO/sub4api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type poolErrorSettings struct {
	SettingRepository
	fail bool
}

func (r *poolErrorSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	if r.fail {
		return nil, context.DeadlineExceeded
	}
	return map[string]string{SettingKeyPoolUserVisibleGroupID: "3", SettingKeyPoolUserSyntheticCount: "2", SettingKeyPoolUserSyntheticBanned: "1"}, nil
}

type poolErrorStats struct {
	poolTestUsageRepo
	fail bool
}

func (r *poolErrorStats) GetUserAccountWindowStatsBatch(ctx context.Context, user int64, ids []int64, start time.Time) (map[int64]*usagestats.AccountStats, error) {
	if r.fail {
		return nil, context.DeadlineExceeded
	}
	return r.poolTestUsageRepo.GetUserAccountWindowStatsBatch(ctx, user, ids, start)
}
func (r *poolErrorStats) GetAccountWindowStatsBatch(context.Context, []int64, time.Time) (map[int64]*usagestats.AccountStats, error) {
	return nil, context.DeadlineExceeded
}
func (r *poolErrorStats) GetPoolGlobalWindowStats(context.Context, time.Time) (*usagestats.AccountStats, error) {
	return nil, context.DeadlineExceeded
}

type poolErrorConcurrency struct{ ConcurrencyCache }

func (poolErrorConcurrency) GetAccountConcurrencyBatch(context.Context, []int64) (map[int64]int, error) {
	return nil, context.DeadlineExceeded
}

func TestAccountPoolReadFailuresAreUnavailable(t *testing.T) {
	ctx := context.Background()
	settings := &poolErrorSettings{fail: true}
	repo := &poolErrorStats{}
	a := &Account{ID: 9, Name: "test", Type: "oauth", Groups: []*Group{{ID: 4}}}
	svc := NewAccountPoolService(poolTestAdmin{accounts: []*Account{a}}, nil, nil, repo, NewSettingService(settings, nil))
	result, err := svc.ListAccountsForUser(ctx, 42, PoolAccountFilter{})
	require.Nil(t, result)
	require.True(t, infraerrors.IsServiceUnavailable(err))
	overview, err := svc.OverviewForUser(ctx, 42)
	require.Nil(t, overview)
	require.True(t, infraerrors.IsServiceUnavailable(err))
	settings.fail = false
	plan, group, err := svc.poolDisplayConfiguration(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(3), group)
	require.Equal(t, 2, plan.Count)
	require.Equal(t, 1, plan.Banned)
	svc.settingService = nil
	repo.fail = true
	result, err = svc.ListAccountsForUser(ctx, 42, PoolAccountFilter{})
	require.Nil(t, result)
	require.True(t, infraerrors.IsServiceUnavailable(err))
	overview, err = svc.OverviewForUser(ctx, 42)
	require.Nil(t, overview)
	require.True(t, infraerrors.IsServiceUnavailable(err))
	items := []PoolAccount{{ID: 9}}
	ids := []int64{9}
	require.True(t, infraerrors.IsServiceUnavailable(svc.fillTodayStats(ctx, items, ids)))
	require.True(t, infraerrors.IsServiceUnavailable(svc.fillWindowStats(ctx, items, ids)))
	_, err = svc.poolGlobalWindowStats(ctx, time.Now())
	require.True(t, infraerrors.IsServiceUnavailable(err))
	svc.concurrencyService = NewConcurrencyService(poolErrorConcurrency{})
	require.True(t, infraerrors.IsServiceUnavailable(svc.fillConcurrency(ctx, items, ids)))
	require.True(t, infraerrors.IsServiceUnavailable(svc.fillUserCapacity(ctx, []PoolUserAccount{{ID: 9}}, ids)))
}
