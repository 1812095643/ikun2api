//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProxyFailureGuardRepositoryPersistsAndCASRecoversWithoutClearingOtherBlocks(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	accounts := newAccountRepositoryWithSQL(client, tx, nil)
	proxies := newProxyRepositoryWithSQL(client, tx)
	proxy := &service.Proxy{Name: "guard-test-proxy", Protocol: "http", Host: "127.0.0.1", Port: 1080, Status: service.StatusActive}
	require.NoError(t, proxies.Create(ctx, proxy))
	a := mustCreateAccount(t, client, &service.Account{Name: "proxy-safety-account", Platform: service.PlatformAnthropic,
		Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, ProxyID: &proxy.ID})
	snapshot, err := accounts.GetAccountProxySnapshot(ctx, a.ID)
	require.NoError(t, err)
	now := time.Now()
	until := now.Add(service.ProxyFailureCooldown)
	state := service.ProxyFailureState{Generation: "first", NotBefore: until, RetryAfter: until, ProbeURL: "https://api.anthropic.com/"}
	ok, err := accounts.QuarantineAccountProxy(ctx, snapshot, state)
	require.NoError(t, err)
	require.True(t, ok)
	current, err := accounts.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.True(t, current.IsProxyQuarantined())
	require.False(t, current.IsSchedulable())
	latest, err := accounts.GetAccountProxySnapshot(ctx, a.ID)
	require.NoError(t, err)
	ok, err = accounts.ClearAccountProxyQuarantine(ctx, latest)
	require.NoError(t, err)
	require.False(t, ok, "cooldown cannot be skipped")
	// A stale admin edit cannot drop the runtime marker.
	current.Extra = map[string]any{"admin_setting": true}
	require.NoError(t, accounts.Update(ctx, current))
	current, err = accounts.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.True(t, current.IsProxyQuarantined())
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(extra,'{proxy_failure_guard,not_before}',to_jsonb($2::text)),
	 temp_unschedulable_until=NOW()+interval '1 hour',temp_unschedulable_reason='manual-other-block' WHERE id=$1`, a.ID, now.Add(-time.Minute).Format(time.RFC3339Nano))
	require.NoError(t, err)
	latest, err = accounts.GetAccountProxySnapshot(ctx, a.ID)
	require.NoError(t, err)
	stale := *latest
	staleState := *latest.State
	staleState.Generation = "older"
	stale.State = &staleState
	ok, err = accounts.ClearAccountProxyQuarantine(ctx, &stale)
	require.NoError(t, err)
	require.False(t, ok, "stale probe cannot clear a new failure")
	oldProxy := *latest.Proxy
	_, err = tx.ExecContext(ctx, `UPDATE proxies SET updated_at=updated_at+interval '1 second' WHERE id=$1`, proxy.ID)
	require.NoError(t, err)
	latest.Proxy = &oldProxy
	ok, err = accounts.ClearAccountProxyQuarantine(ctx, latest)
	require.NoError(t, err)
	require.False(t, ok, "route changed during probe")
	latest, err = accounts.GetAccountProxySnapshot(ctx, a.ID)
	require.NoError(t, err)
	ok, err = accounts.ClearAccountProxyQuarantine(ctx, latest)
	require.NoError(t, err)
	require.True(t, ok)
	current, err = accounts.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.False(t, current.IsProxyQuarantined())
	require.Equal(t, "manual-other-block", current.TempUnschedulableReason)
	require.False(t, current.IsSchedulable())
}
