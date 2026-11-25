-- Versioned, column-restricted evidence contract. No raw content/credentials are exported.
-- Provisioning a LOGIN role is intentionally separate (deploy/ops-monitor/grants.sql).
CREATE OR REPLACE VIEW ops_monitor_v1_errors AS
SELECT id,created_at AS observed_at,jsonb_build_object(
 'kind','request.error','request_id',COALESCE(client_request_id,request_id,''),
 'user_id',user_id,'key_id',api_key_id,'account_id',account_id,'group_id',group_id,
 'platform',platform,'model',model,'upstream_status',upstream_status_code,
 'reason_code',CASE WHEN error_message='api key 日限额已用完' AND is_business_limited AND COALESCE(upstream_status_code,0)=0 THEN 'key_daily_limit'
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

CREATE OR REPLACE VIEW ops_monitor_v1_usage AS
SELECT id,created_at AS observed_at,jsonb_build_object('kind','usage.recorded','request_id',COALESCE(request_id,''),
 'user_id',user_id,'key_id',api_key_id,'account_id',account_id,'group_id',group_id,'channel_id',channel_id,'model',model,
 'metrics',jsonb_strip_nulls(jsonb_build_object('duration_ms',duration_ms,'first_token_ms',first_token_ms,'client_first_token_ms',client_first_token_ms,
 'pre_forward_ms',pre_forward_ms,'queue_ms',gateway_wait_ms,'upload_ms',upload_ms,'switches',account_switch_count,'model_mismatch',CASE WHEN upstream_model_mismatch THEN 1 ELSE 0 END,'billed_amount',actual_cost))) AS evidence
FROM usage_logs;

CREATE OR REPLACE VIEW ops_monitor_v1_accounts AS
SELECT id,updated_at AS observed_at,jsonb_build_object('kind','account.snapshot','account_id',id,'platform',platform,
 'reason_code',CASE WHEN status='error' THEN 'account_error' WHEN temp_unschedulable_until>now() THEN 'temporarily_unavailable' WHEN NOT schedulable THEN 'not_schedulable' ELSE 'unclassified' END,
 'metrics',jsonb_build_object('schedulable',CASE WHEN schedulable AND status='active' AND (temp_unschedulable_until IS NULL OR temp_unschedulable_until<=now()) THEN 1 ELSE 0 END)) AS evidence
FROM accounts WHERE deleted_at IS NULL;

CREATE OR REPLACE VIEW ops_monitor_v1_balances AS
SELECT id,updated_at AS observed_at,jsonb_build_object('kind','quota.snapshot','user_id',id,'reason_code','balance',
 'metrics',jsonb_build_object('remaining',balance,'frozen',frozen_balance)) AS evidence
FROM users u WHERE deleted_at IS NULL AND status='active'
AND EXISTS(SELECT 1 FROM api_keys k WHERE k.user_id=u.id AND k.deleted_at IS NULL AND k.status='active' AND k.funding_source='balance');

CREATE OR REPLACE VIEW ops_monitor_v1_subscriptions AS
SELECT id,updated_at AS observed_at,jsonb_build_object('kind','quota.snapshot','user_id',user_id,'reason_code','subscription',
 'task_id','subscription:'||id,
 'metrics',jsonb_strip_nulls(jsonb_build_object('remaining',total_credit_usd-total_used_usd,'total',total_credit_usd,
 'daily_remaining',CASE WHEN daily_limit_usd>0 THEN daily_limit_usd-CASE WHEN date_trunc('day',now())>daily_window_start THEN 0 ELSE daily_used_usd END END,'weekly_remaining',CASE WHEN weekly_limit_usd>0 THEN weekly_limit_usd-CASE WHEN now()>=weekly_window_start+interval '168 hours' THEN 0 ELSE weekly_used_usd END END,'monthly_remaining',CASE WHEN monthly_limit_usd>0 THEN monthly_limit_usd-CASE WHEN now()>=monthly_window_start+interval '720 hours' THEN 0 ELSE monthly_used_usd END END))) AS evidence
FROM universal_subscriptions WHERE status='active' AND starts_at<=now() AND expires_at>now();

CREATE OR REPLACE VIEW ops_monitor_v1_payments AS
SELECT id,updated_at AS observed_at,jsonb_build_object('kind','payment.snapshot','user_id',user_id,'task_id','payment:'||id,
 'metrics',jsonb_build_object('paid',CASE WHEN paid_at IS NOT NULL AND status IN ('paid','recharging','completed','failed') THEN 1 ELSE 0 END,
 'fulfilled',CASE WHEN completed_at IS NOT NULL AND status='completed' THEN 1 ELSE 0 END,'paid_at_ms',extract(epoch from paid_at)*1000)) AS evidence
FROM payment_orders WHERE paid_at IS NOT NULL;

-- UUID is preserved without lossy hashing in the stable keyset cursor.
CREATE OR REPLACE VIEW ops_monitor_v1_holds AS
SELECT id::text AS id,updated_at AS observed_at,
jsonb_build_object('kind','billing.hold','user_id',user_id,'key_id',api_key_id,'task_id',id::text,
 'metrics',jsonb_build_object('reserved',CASE WHEN state='reserved' THEN 1 ELSE 0 END,'expires_at_ms',extract(epoch from expires_at)*1000)) AS evidence
FROM universal_subscription_holds;

CREATE OR REPLACE VIEW ops_monitor_v1_system AS
SELECT id,created_at AS observed_at,jsonb_build_object('kind',CASE
 WHEN message='proxy_failure_guard.account_quarantined' THEN 'account.quarantined'
 WHEN message='proxy_failure_guard.account_recovered' THEN 'account.recovered'
 WHEN message LIKE '%refresh%failed%' THEN 'account.refresh_failed'
 WHEN message IN ('grok_media.store_video_pending_billing_failed','grok_media.video_billing_skipped_missing_pending','grok_media.persist_funding_failed') THEN 'billing.metadata_failed'
 ELSE 'system.error' END,'account_id',account_id,'user_id',user_id,'platform',platform,'model',model,
 'request_id',COALESCE(client_request_id,request_id,''),'reason_code','legacy_log') AS evidence
FROM ops_system_logs WHERE message IN ('proxy_failure_guard.account_quarantined','proxy_failure_guard.account_recovered',
 'grok_media.store_video_pending_billing_failed','grok_media.video_billing_skipped_missing_pending','grok_media.persist_funding_failed') OR message LIKE '%refresh%failed%';

-- The dedup claim is committed in the same transaction as billing effects.
CREATE OR REPLACE VIEW ops_monitor_v1_billing AS
SELECT d.id,d.created_at AS observed_at,jsonb_build_object('kind','billing.applied','user_id',k.user_id,'key_id',d.api_key_id,
 'task_id',substring(d.request_id FROM 12),'reason_code','durable_billing_confirmation','metrics',jsonb_build_object('billing_confirmed',1)) AS evidence
FROM usage_billing_dedup d LEFT JOIN api_keys k ON k.id=d.api_key_id WHERE d.request_id LIKE 'grok-video:%';

-- Universal charges and the independently persisted usage amount share request/key identity.
-- Wallet balance has no equivalent per-charge amount ledger in this schema: do not infer one.
CREATE OR REPLACE VIEW ops_monitor_v1_reconciliation AS
SELECT c.id,GREATEST(c.created_at,u.created_at) AS observed_at,
jsonb_build_object('kind','billing.reconciliation','user_id',u.user_id,'key_id',c.api_key_id,
 'task_id',CASE WHEN c.request_id LIKE 'grok-video:%' THEN substring(c.request_id FROM 12) ELSE c.request_id END,
 'metrics',jsonb_build_object('expected',u.actual_cost,'actual',c.amount,
 'confirmed_duplicate',CASE WHEN count(*) OVER(PARTITION BY c.request_id,c.api_key_id)>1 THEN 1 ELSE 0 END,
 'reconciled',CASE WHEN abs(u.actual_cost-c.amount)<=0.000001 THEN 1 ELSE 0 END)) AS evidence
FROM universal_subscription_charges c JOIN usage_logs u ON u.request_id=c.request_id AND u.api_key_id=c.api_key_id
WHERE c.created_at>=now()-interval '7 days' AND u.created_at<now()-interval '120 seconds';

-- Every retained attempt matters: a final 502 must not hide an earlier 429.
-- Project numeric/enum fields only; never export message/detail/URL/body.
CREATE OR REPLACE VIEW ops_monitor_v1_attempts AS
SELECT e.id::text||':'||a.ordinality::text AS id,e.created_at AS observed_at,
jsonb_build_object('kind',CASE WHEN a.event->>'kind'='stream_failed' THEN 'stream.failed' ELSE 'attempt.failed' END,
 'request_id',COALESCE(e.client_request_id,e.request_id,''),'attempt_id','legacy:'||e.id::text||':'||a.ordinality::text,
 'user_id',e.user_id,'key_id',e.api_key_id,'group_id',e.group_id,'model',e.model,'platform',e.platform,
 'account_id',CASE WHEN (a.event->>'account_id')~'^[0-9]{1,15}$' THEN (a.event->>'account_id')::bigint ELSE e.account_id END,
 'upstream_status',CASE WHEN (a.event->>'upstream_status_code')~'^[1-5][0-9]{2}$' THEN (a.event->>'upstream_status_code')::int END,
 'reason_code',CASE WHEN a.event->>'reason' IN ('dns','connect','tls','proxy','timeout','transport_unknown') THEN a.event->>'reason'
 WHEN a.event->>'kind'='request_error' THEN 'transport_unknown' ELSE 'legacy_attempt' END,
 'metrics',jsonb_build_object('legacy_partial',1,'intended_status',e.status_code)) AS evidence
FROM ops_error_logs e CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(e.upstream_errors)='array' THEN e.upstream_errors ELSE '[]'::jsonb END) WITH ORDINALITY AS a(event,ordinality)
WHERE a.event->>'kind' IN ('failover','http_error','request_error','retry_exhausted','stream_failed');

CREATE OR REPLACE VIEW ops_monitor_v1_metrics AS
SELECT id,created_at AS observed_at,jsonb_build_object('kind','host.resources','task_id','host','metrics',jsonb_strip_nulls(jsonb_build_object('memory_used_percent',memory_usage_percent,'cpu_percent',cpu_usage_percent))) AS evidence
FROM ops_system_metrics WHERE platform IS NULL AND group_id IS NULL;
CREATE OR REPLACE VIEW ops_monitor_v1_dependencies AS
SELECT id::text || ':' || dependency AS id,created_at AS observed_at,jsonb_build_object('kind','dependency.health','task_id',dependency,'reason_code','existing_probe','metrics',jsonb_build_object('healthy',CASE WHEN healthy THEN 1 ELSE 0 END)) AS evidence
FROM ops_system_metrics CROSS JOIN LATERAL (VALUES ('postgres',db_ok),('redis',redis_ok)) AS dependency_state(dependency,healthy)
WHERE platform IS NULL AND group_id IS NULL AND healthy IS NOT NULL;
