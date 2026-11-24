-- Versioned, column-restricted evidence contract. No raw content/credentials are exported.
-- Provisioning a LOGIN role is intentionally separate (deploy/ops-monitor/grants.sql).
CREATE OR REPLACE VIEW ops_monitor_v1_errors AS
SELECT id,created_at AS observed_at,jsonb_build_object(
 'kind','request.error','request_id',COALESCE(client_request_id,request_id,''),
 'user_id',user_id,'key_id',api_key_id,'account_id',account_id,'group_id',group_id,
 'platform',platform,'model',model,'upstream_status',upstream_status_code,
 'reason_code',CASE WHEN COALESCE(NULLIF(upstream_status_code,0),status_code)=400 AND (COALESCE(error_message,'')||COALESCE(upstream_error_message,'')) ILIKE '%Invalid%signature%thinking%' THEN 'thinking_signature_invalid'
 WHEN error_message='api key 日限额已用完' AND is_business_limited AND COALESCE(upstream_status_code,0)=0 THEN 'key_daily_limit'
 WHEN provider_error_code IN ('insufficient_quota','insufficient_balance','quota_exhausted') THEN 'upstream_quota_exhausted'
 WHEN error_phase IN ('client','request') AND COALESCE(upstream_status_code,0)=0 AND
 (error_type IN ('INSUFFICIENT_BALANCE','insufficient_balance') OR (regexp_match(error_body,'"code"\s*:\s*"(INSUFFICIENT_BALANCE|insufficient_balance)"'))[1] IS NOT NULL) THEN 'user_balance_exhausted'
 WHEN error_phase IN ('client','request') AND COALESCE(upstream_status_code,0)=0 AND (regexp_match(error_body,'"code"\s*:\s*"(API_KEY_RATE_5H_EXCEEDED|API_KEY_RATE_7D_EXCEEDED)"'))[1] IS NOT NULL THEN 'key_window_limit'
 WHEN network_error_type IN ('dns','connect','tls','proxy','timeout') THEN network_error_type
 WHEN error_phase='routing' AND status_code=503 AND error_message ILIKE 'No available%' THEN 'pool_empty'
 WHEN error_phase IN ('request','routing') AND status_code=429 AND error_message ILIKE '%concurrency%' THEN 'slot_timeout'
 WHEN error_type='model_not_found' THEN 'model_unavailable'
 WHEN error_phase='upstream' AND COALESCE(upstream_status_code,0)=0 THEN 'upstream_failure'
 WHEN error_phase='request' AND status_code>=400 AND status_code<500 THEN 'client_request_rejected'
 WHEN status_code>=400 AND COALESCE(upstream_status_code,0)=0 THEN 'request_failure'
 ELSE 'unclassified' END,
 'metrics',jsonb_strip_nulls(jsonb_build_object('intended_status',status_code,'duration_ms',duration_ms,'first_token_ms',time_to_first_token_ms,'legacy_partial',1,'stream',CASE WHEN stream THEN 1 ELSE 0 END))) AS evidence
FROM ops_error_logs;
