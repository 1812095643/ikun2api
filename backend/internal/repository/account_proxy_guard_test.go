package repository

import (
	"context"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProxyFailureGuardRepositoryAtomicQuarantineAndRecoveryContracts(t *testing.T) {
	repo := newAccountRepositoryWithSQL(nil, &recordingSQLExecutor{result: rowsAffectedResult(0)}, nil)
	store := repo.sql.(*recordingSQLExecutor)
	id := int64(8)
	now := time.Now()
	snapshot := &service.AccountProxySnapshot{AccountID: 7, ProxyID: &id, Proxy: &service.Proxy{ID: id, UpdatedAt: now},
		State: &service.ProxyFailureState{Generation: "old-generation", NotBefore: now.Add(-time.Minute)}}
	_, err := repo.QuarantineAccountProxy(context.Background(), snapshot, *snapshot.State)
	require.NoError(t, err)
	require.Len(t, store.execQueries, 1)
	q := normalizeSQLWhitespace(store.execQueries[0])
	for _, part := range []string{"WITH updated AS", "proxy_failure_guard", "a.proxy_id IS NOT DISTINCT FROM $5", "p.updated_at=$6", "proxy_failure_guard_reset_epoch','')=$8", "INSERT INTO scheduler_outbox"} {
		require.Contains(t, q, part)
	}
	_, err = repo.ClearAccountProxyQuarantine(context.Background(), snapshot)
	require.NoError(t, err)
	require.Len(t, store.execQueries, 2)
	q = normalizeSQLWhitespace(store.execQueries[1])
	for _, part := range []string{"a.status='active'", "a.proxy_id=$2", "'generation'=$3", "p.updated_at=$4", "'not_before')::timestamptz <= NOW()", "ELSE a.temp_unschedulable_until", "INSERT INTO scheduler_outbox"} {
		require.Contains(t, q, part)
	}
	_, err = repo.ResetAccountProxyQuarantine(context.Background(), snapshot)
	require.NoError(t, err)
	require.Len(t, store.execQueries, 3)
	q = normalizeSQLWhitespace(store.execQueries[2])
	for _, part := range []string{"-'proxy_failure_guard'", "'generation' IS NOT DISTINCT FROM $3::text", "reset_epoch','')=$2", "a.proxy_id IS NOT DISTINCT FROM $4::bigint", "to_jsonb($5::text)", "ELSE a.temp_unschedulable_until", "INSERT INTO scheduler_outbox"} {
		require.Contains(t, q, part)
	}
	require.NotContains(t, q, "not_before", "manual recovery does not wait for cooldown")
	require.NotContains(t, q, "state='active'", "manual recovery can reset active quarantine regardless of elapsed cooldown")
	require.NotContains(t, q, "extra->'proxy_failure_guard' IS NOT NULL", "reset also advances the epoch if a failure was never persisted")
}

func TestProxyFailureGuardSchedulerProjectionAndForgedUpdates(t *testing.T) {
	extra := map[string]any{service.ProxyFailureGuardExtraKey: map[string]any{"generation": "runtime"}, "keep": true}
	projected := filterSchedulerExtra(extra)
	require.Contains(t, projected, service.ProxyFailureGuardExtraKey)
	stripped := stripProxyFailureGuardUpdate(extra)
	require.NotContains(t, stripped, service.ProxyFailureGuardExtraKey)
	require.Equal(t, true, stripped["keep"])
	require.Contains(t, extra, service.ProxyFailureGuardExtraKey, "caller map is immutable")
}
