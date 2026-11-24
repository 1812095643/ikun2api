package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/MACOS-DO/sub4api/internal/service"
)

func (r *accountRepository) PauseOpenAIOAuthIfUnchanged(ctx context.Context, expected *service.Account, message string, autoRetry bool) (bool, error) {
	if r == nil || r.sql == nil || !service.IsOpenAIOAuthReloginAccount(expected) {
		return false, errors.New("OpenAI OAuth recovery repository is unavailable")
	}
	credentials, err := json.Marshal(normalizeJSONMap(expected.Credentials))
	if err != nil {
		return false, err
	}
	state, err := json.Marshal(expected.Extra[service.OpenAIOAuthRecoveryExtraKey])
	if err != nil {
		return false, err
	}
	fingerprint := service.OpenAIOAuthRecoveryFingerprint(expected)
	result, err := r.sql.ExecContext(ctx, `WITH changed AS (
	 UPDATE accounts SET status='error', schedulable=FALSE, error_message=$1, updated_at=NOW(),
	 extra=jsonb_set(COALESCE(extra,'{}'::jsonb),'{openai_oauth_recovery}',
	 jsonb_build_object('fingerprint',$2::text,
	 'resume_schedulable',CASE WHEN extra#>>'{openai_oauth_recovery,fingerprint}'=$2
	   THEN COALESCE((extra#>>'{openai_oauth_recovery,resume_schedulable}')::boolean,FALSE)
	   ELSE status='active' AND schedulable END,
	 'auto_retry',$3::boolean,'reason',$1::text,'paused_at',NOW()))
	 WHERE id=$4 AND deleted_at IS NULL AND platform='openai' AND type='oauth' AND parent_account_id IS NULL
	 AND credentials=$5::jsonb AND proxy_id IS NOT DISTINCT FROM $6
	 AND status=$8 AND schedulable=$9 AND COALESCE(error_message,'')=$10
	 AND COALESCE(extra->'openai_oauth_recovery','null'::jsonb)=$11::jsonb
	 AND (status='active' OR (status='error' AND $12::boolean))
	 RETURNING id
	 ) INSERT INTO scheduler_outbox(event_type,account_id,group_id,payload)
	 SELECT $7,id,NULL,NULL FROM changed`, message, fingerprint, autoRetry, expected.ID, string(credentials), expected.ProxyID,
		service.SchedulerOutboxEventAccountChanged, expected.Status, expected.Schedulable, expected.ErrorMessage, string(state), service.OpenAIOAuthRecoveryPending(expected))
	return r.finishOpenAIRecoveryMutation(ctx, expected.ID, result, err)
}

func (r *accountRepository) ApplyOpenAIOAuthReloginIfUnchanged(ctx context.Context, expected *service.Account, credentials map[string]any) (bool, error) {
	if r == nil || r.sql == nil || !service.IsOpenAIOAuthReloginAccount(expected) {
		return false, errors.New("OpenAI OAuth recovery repository is unavailable")
	}
	before, err := json.Marshal(normalizeJSONMap(expected.Credentials))
	if err != nil {
		return false, err
	}
	after, err := json.Marshal(normalizeJSONMap(credentials))
	if err != nil {
		return false, err
	}
	state, err := json.Marshal(expected.Extra[service.OpenAIOAuthRecoveryExtraKey])
	if err != nil {
		return false, err
	}
	owned := expected.Status == service.StatusError && hasOpenAIRecoveryState(expected)
	clearAuthError := owned || expected.Status == service.StatusError && service.IsOpenAIOAuthCredentialError(expected.ErrorMessage)
	free, _ := credentials["plan_type"].(string)
	free = strings.ToLower(strings.TrimSpace(free))
	result, err := r.sql.ExecContext(ctx, `WITH changed AS (
	 UPDATE accounts SET credentials=$1::jsonb,
	 status=CASE WHEN $13::boolean THEN 'active' ELSE status END,
	 error_message=CASE WHEN $13::boolean THEN '' ELSE error_message END,
	 schedulable=CASE WHEN $12::boolean THEN FALSE WHEN $11::boolean THEN
	 COALESCE((extra#>>'{openai_oauth_recovery,resume_schedulable}')::boolean,FALSE) ELSE schedulable END,
	 extra=COALESCE(extra,'{}'::jsonb)-'openai_oauth_recovery', updated_at=NOW()
	 WHERE id=$3 AND deleted_at IS NULL AND platform='openai' AND type='oauth' AND parent_account_id IS NULL
	 AND credentials=$4::jsonb AND proxy_id IS NOT DISTINCT FROM $5
	 AND status=$7 AND schedulable=$8 AND COALESCE(error_message,'')=$9
	 AND COALESCE(extra->'openai_oauth_recovery','null'::jsonb)=$10::jsonb
	 AND (NOT $11::boolean OR extra#>>'{openai_oauth_recovery,fingerprint}'=$2)
	 AND status IN ('active','disabled','error')
	 RETURNING id
	 ) INSERT INTO scheduler_outbox(event_type,account_id,group_id,payload)
	 SELECT $6,id,NULL,NULL FROM changed`, string(after), service.OpenAIOAuthRecoveryFingerprint(expected), expected.ID, string(before), expected.ProxyID,
		service.SchedulerOutboxEventAccountChanged, expected.Status, expected.Schedulable, expected.ErrorMessage, string(state), owned, free == "free", clearAuthError)
	return r.finishOpenAIRecoveryMutation(ctx, expected.ID, result, err)
}

func hasOpenAIRecoveryState(account *service.Account) bool {
	state, ok := account.Extra[service.OpenAIOAuthRecoveryExtraKey].(map[string]any)
	return ok && state["fingerprint"] == service.OpenAIOAuthRecoveryFingerprint(account) && state["reason"] == account.ErrorMessage
}

type openAIRecoverySQLResult interface {
	RowsAffected() (int64, error)
}

func (r *accountRepository) finishOpenAIRecoveryMutation(ctx context.Context, id int64, result openAIRecoverySQLResult, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	r.syncSchedulerAccountSnapshotDetached(ctx, id)
	return true, nil
}
