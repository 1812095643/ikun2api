package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type poolShareMemory struct {
	UsageLogRepository
	user    int64
	hash    string
	expires time.Time
	active  bool
}

func (r *poolShareMemory) SavePoolShare(_ context.Context, id int64, hash string, expires time.Time) error {
	r.user = id
	r.hash = hash
	r.expires = expires
	return nil
}
func (r *poolShareMemory) ResolvePoolShare(_ context.Context, hash string, now time.Time) (int64, error) {
	if r.active && hash == r.hash && now.Before(r.expires) {
		return r.user, nil
	}
	return 0, nil
}
func (r *poolShareMemory) RevokePoolShare(_ context.Context, id int64) error {
	if id == r.user {
		r.hash = ""
	}
	return nil
}

func TestAccountPoolShareTokenScopeRotationAndExpiry(t *testing.T) {
	r := &poolShareMemory{active: true}
	s := NewAccountPoolService(nil, nil, nil, r, nil)
	ctx := context.Background()
	first, err := s.CreateShareLink(ctx, 42)
	require.NoError(t, err)
	require.Len(t, first.Token, 48)
	require.NotEqual(t, first.Token, r.hash)
	id, err := s.ResolveShareLink(ctx, first.Token)
	require.NoError(t, err)
	require.Equal(t, int64(42), id)
	second, err := s.CreateShareLink(ctx, 42)
	require.NoError(t, err)
	require.NotEqual(t, first.Token, second.Token)
	_, err = s.ResolveShareLink(ctx, first.Token)
	require.Error(t, err)
	r.active = false
	_, err = s.ResolveShareLink(ctx, second.Token)
	require.Error(t, err)
	r.active = true
	r.expires = time.Now().Add(-time.Minute)
	_, err = s.ResolveShareLink(ctx, second.Token)
	require.Error(t, err)
	third, err := s.CreateShareLink(ctx, 42)
	require.NoError(t, err)
	require.NoError(t, s.RevokeShareLink(ctx, 42))
	_, err = s.ResolveShareLink(ctx, third.Token)
	require.Error(t, err)
	for _, token := range []string{"", "sk-TEST", "Bearer login-token"} {
		_, err = s.ResolveShareLink(ctx, token)
		require.Error(t, err)
	}
}

func TestAccountPoolPublicProjectionExcludesInternalDetails(t *testing.T) {
	input := []PoolUserAccount{{ID: 88, Name: "mas***ed", Platform: "anthropic", Status: "active", Today: PoolWindow{Cost: 999}, Capacity: PoolUserCapacity{InUse: 8}, Groups: []PoolGroupRef{{ID: 99, Name: "private"}}, Synthetic: true}}
	body, err := json.Marshal(PoolPublicAccounts(input))
	require.NoError(t, err)
	var items []map[string]any
	require.NoError(t, json.Unmarshal(body, &items))
	require.Len(t, items[0], 6)
	require.Len(t, items[0]["capacity"].(map[string]any), 2)
	for _, key := range []string{"id", "today", "groups", "synthetic", "credentials", "schedulable", "last_used_at"} {
		require.NotContains(t, items[0], key)
	}
}

func TestAccountPoolUserGroupScopesEveryStatisticsQuery(t *testing.T) {
	ctx := context.Background()
	repo := &poolTestUsageRepo{}
	settings := NewSettingService(&poolErrorSettings{}, nil)
	acc := &Account{ID: 9, Name: "visible", Platform: "anthropic", Type: "oauth", Groups: []*Group{{ID: 3, Name: "selected"}, {ID: 4, Name: "hidden"}}}
	s := NewAccountPoolService(poolTestAdmin{accounts: []*Account{acc}}, nil, nil, repo, settings)
	rows, err := s.ListAccountsForUser(ctx, 42, PoolAccountFilter{})
	require.NoError(t, err)
	// 只展示管理员开放的分组，账号在其它分组里的归属不外露。
	require.Equal(t, []PoolGroupRef{{ID: 3, Name: "selected"}}, rows.Items[0].Groups)
	_, err = s.OverviewForUser(ctx, 42)
	require.NoError(t, err)
	// 号池列表与统计查询都必须收敛到可见分组。
	require.NotEmpty(t, repo.groups)
	for _, id := range repo.groups {
		require.Equal(t, int64(3), id)
	}
}
