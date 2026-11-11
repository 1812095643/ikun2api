package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/google/uuid"
)

func stripProxyFailureGuardUpdate(extra map[string]any) map[string]any {
	_, hasGuard := extra[service.ProxyFailureGuardExtraKey]
	_, hasEpoch := extra[service.ProxyFailureResetEpochExtraKey]
	if !hasGuard && !hasEpoch {
		return extra
	}
	copy := make(map[string]any, len(extra))
	for key, value := range extra {
		if key != service.ProxyFailureGuardExtraKey && key != service.ProxyFailureResetEpochExtraKey {
			copy[key] = value
		}
	}
	return copy
}

func (r *accountRepository) GetAccountProxySnapshot(ctx context.Context, id int64) (*service.AccountProxySnapshot, error) {
	rows, err := r.sql.QueryContext(ctx, `
		SELECT a.id,a.platform,a.type,a.status,a.proxy_id,a.proxy_fallback_origin_id,
		a.extra -> 'proxy_failure_guard',COALESCE(a.extra->>'proxy_failure_guard_reset_epoch',''),p.id,p.protocol,p.host,p.port,
			COALESCE(p.username,''),COALESCE(p.password,''),p.status,p.expires_at,p.updated_at
		FROM accounts a LEFT JOIN proxies p ON p.id=a.proxy_id AND p.deleted_at IS NULL
		WHERE a.id=$1 AND a.deleted_at IS NULL`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, service.ErrAccountNotFound
	}
	var s service.AccountProxySnapshot
	var status string
	var state []byte
	var proxyID, originID, loadedID, port sql.NullInt64
	var resetEpoch, protocol, host, username, password, proxyStatus sql.NullString
	var expires, updated sql.NullTime
	if err := rows.Scan(&s.AccountID, &s.Platform, &s.Type, &status, &proxyID, &originID, &state,
		&resetEpoch, &loadedID, &protocol, &host, &port, &username, &password, &proxyStatus, &expires, &updated); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.Active = status == service.StatusActive
	s.ResetEpoch = resetEpoch.String
	if proxyID.Valid {
		s.ProxyID = &proxyID.Int64
	}
	if originID.Valid {
		s.OriginID = &originID.Int64
	}
	if loadedID.Valid {
		s.Proxy = &service.Proxy{ID: loadedID.Int64, Protocol: protocol.String, Host: host.String,
			Port: int(port.Int64), Username: username.String, Password: password.String,
			Status: proxyStatus.String, UpdatedAt: updated.Time}
		if expires.Valid {
			s.Proxy.ExpiresAt = &expires.Time
		}
	}
	s.State, err = service.DecodeProxyFailureState(state)
	return &s, err
}

func (r *accountRepository) QuarantineAccountProxy(ctx context.Context, snapshot *service.AccountProxySnapshot, state service.ProxyFailureState) (bool, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return false, err
	}
	var proxyUpdated *time.Time
	if snapshot.Proxy != nil {
		proxyUpdated = &snapshot.Proxy.UpdatedAt
	}
	result, err := r.sql.ExecContext(ctx, `
		WITH updated AS (
		 UPDATE accounts a SET
		  extra=jsonb_set(COALESCE(a.extra,'{}'::jsonb),'{proxy_failure_guard}',$2::jsonb,true),
		  temp_unschedulable_until=GREATEST(a.temp_unschedulable_until,$3::timestamptz),
		  temp_unschedulable_reason=CASE WHEN a.temp_unschedulable_until IS NULL
		    OR a.temp_unschedulable_until <= $3 OR a.temp_unschedulable_reason LIKE 'proxy_failure_guard:%'
		    THEN $4 ELSE a.temp_unschedulable_reason END,
		  updated_at=NOW()
		 WHERE a.id=$1 AND a.deleted_at IS NULL AND a.proxy_id IS NOT DISTINCT FROM $5::bigint
		  AND ($6::timestamptz IS NULL OR EXISTS(SELECT 1 FROM proxies p
		    WHERE p.id=a.proxy_id AND p.deleted_at IS NULL AND p.updated_at=$6))
		  AND (NOT (COALESCE(a.extra,'{}'::jsonb) ? 'proxy_failure_guard')
		    OR a.extra->'proxy_failure_guard'->>'generation'=$7)
		  AND COALESCE(a.extra->>'proxy_failure_guard_reset_epoch','')=$8
		 RETURNING a.id
		)
		INSERT INTO scheduler_outbox(event_type,account_id,group_id,payload)
		SELECT $9,id,NULL,NULL FROM updated`, snapshot.AccountID, string(payload), state.NotBefore,
		service.ProxyFailureReason, snapshot.ProxyID, proxyUpdated,
		state.Generation, snapshot.ResetEpoch, service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err == nil && count > 0 {
		r.syncSchedulerAccountSnapshot(ctx, snapshot.AccountID)
	}
	return count > 0, err
}

func (r *accountRepository) ClearAccountProxyQuarantine(ctx context.Context, snapshot *service.AccountProxySnapshot) (bool, error) {
	if snapshot == nil || snapshot.State == nil || snapshot.Proxy == nil || snapshot.ProxyID == nil {
		return false, nil
	}
	// Probe completion does not authorize removing a newer failure, another
	// account blocker, or a route changed while the probe was in flight.
	result, err := r.sql.ExecContext(ctx, `
		WITH updated AS (
		 UPDATE accounts a SET extra=COALESCE(a.extra,'{}'::jsonb)-'proxy_failure_guard',
		  temp_unschedulable_until=CASE WHEN a.temp_unschedulable_reason LIKE 'proxy_failure_guard:%'
		    THEN NULL ELSE a.temp_unschedulable_until END,
		  temp_unschedulable_reason=CASE WHEN a.temp_unschedulable_reason LIKE 'proxy_failure_guard:%'
		    THEN NULL ELSE a.temp_unschedulable_reason END,updated_at=NOW()
		 WHERE a.id=$1 AND a.deleted_at IS NULL AND a.status='active'
		  AND a.proxy_id=$2 AND a.extra->'proxy_failure_guard'->>'generation'=$3
		  AND (a.extra->'proxy_failure_guard'->>'not_before')::timestamptz <= NOW()
		  AND EXISTS(SELECT 1 FROM proxies p WHERE p.id=a.proxy_id AND p.deleted_at IS NULL
		    AND p.status='active' AND (p.expires_at IS NULL OR p.expires_at>NOW()) AND p.updated_at=$4)
		 RETURNING a.id
		)
		INSERT INTO scheduler_outbox(event_type,account_id,group_id,payload)
		SELECT $5,id,NULL,NULL FROM updated`, snapshot.AccountID, *snapshot.ProxyID, snapshot.State.Generation,
		snapshot.Proxy.UpdatedAt, service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err == nil && count > 0 {
		r.syncSchedulerAccountSnapshot(ctx, snapshot.AccountID)
	}
	return count > 0, err
}

func (r *accountRepository) ListProxyQuarantinedAccounts(ctx context.Context, afterID int64, limit int) ([]int64, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT id FROM accounts WHERE deleted_at IS NULL
	 AND id>$1 AND extra ? 'proxy_failure_guard' ORDER BY id LIMIT $2`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ResetAccountProxyQuarantine is reserved for explicit management recovery.
// Compare the observed generation and route so a newer failure is preserved.
func (r *accountRepository) ResetAccountProxyQuarantine(ctx context.Context, snapshot *service.AccountProxySnapshot) (bool, error) {
	if scoped := r.accountTransactionScope(ctx); scoped != r {
		return scoped.ResetAccountProxyQuarantine(ctx, snapshot)
	}
	if snapshot == nil {
		return false, nil
	}
	var generation *string
	if snapshot.State != nil {
		generation = &snapshot.State.Generation
	}
	result, err := r.sql.ExecContext(ctx, `
		WITH updated AS (
		 UPDATE accounts a SET extra=jsonb_set(COALESCE(a.extra,'{}'::jsonb)-'proxy_failure_guard',
		  '{proxy_failure_guard_reset_epoch}',to_jsonb($5::text),true),
		  temp_unschedulable_until=CASE WHEN a.temp_unschedulable_reason LIKE 'proxy_failure_guard:%'
		    THEN NULL ELSE a.temp_unschedulable_until END,
		  temp_unschedulable_reason=CASE WHEN a.temp_unschedulable_reason LIKE 'proxy_failure_guard:%'
		    THEN NULL ELSE a.temp_unschedulable_reason END,updated_at=NOW()
		 WHERE a.id=$1 AND a.deleted_at IS NULL
		  AND a.extra->'proxy_failure_guard'->>'generation' IS NOT DISTINCT FROM $3::text
		  AND COALESCE(a.extra->>'proxy_failure_guard_reset_epoch','')=$2
		  AND a.proxy_id IS NOT DISTINCT FROM $4::bigint
		 RETURNING a.id
		)
		INSERT INTO scheduler_outbox(event_type,account_id,group_id,payload)
		SELECT $6,id,NULL,NULL FROM updated`, snapshot.AccountID, snapshot.ResetEpoch, generation,
		snapshot.ProxyID, uuid.NewString(), service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err == nil && count > 0 {
		r.syncSchedulerAccountSnapshot(ctx, snapshot.AccountID)
	}
	return count > 0, err
}
