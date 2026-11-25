-- Add only numeric attribution evidence; no prompt, credential or identity scope expansion.
CREATE OR REPLACE VIEW ops_monitor_v1_usage AS
SELECT id,created_at AS observed_at,jsonb_build_object('kind','usage.recorded','request_id',COALESCE(request_id,''),
 'user_id',user_id,'key_id',api_key_id,'account_id',account_id,'group_id',group_id,'channel_id',channel_id,'model',model,
 'metrics',jsonb_strip_nulls(jsonb_build_object('duration_ms',duration_ms,'first_token_ms',first_token_ms,'client_first_token_ms',client_first_token_ms,
 'pre_forward_ms',pre_forward_ms,'queue_ms',gateway_wait_ms,'upload_ms',upload_ms,'switches',account_switch_count,'model_mismatch',CASE WHEN upstream_model_mismatch THEN 1 ELSE 0 END,'billed_amount',actual_cost,
 'request_stream',CASE WHEN stream THEN 1 ELSE 0 END,
 'context_input_tokens',(to_jsonb(usage_logs)->>'input_tokens')::bigint,
 'context_cache_read_tokens',(to_jsonb(usage_logs)->>'cache_read_tokens')::bigint,
 'context_cache_write_tokens',(to_jsonb(usage_logs)->>'cache_creation_tokens')::bigint,
 'reasoning_configured',CASE WHEN lower(COALESCE(to_jsonb(usage_logs)->>'reasoning_effort','')) IN ('low','medium','high','xhigh','max') THEN 1 WHEN lower(COALESCE(to_jsonb(usage_logs)->>'reasoning_effort','')) IN ('none','disabled') THEN 0 END,
 'first_event_semantics',CASE WHEN stream AND (to_jsonb(usage_logs)->>'account_platform_snapshot')='anthropic' AND (to_jsonb(usage_logs)->>'upstream_endpoint') LIKE '%/messages' THEN 1 END))) AS evidence
FROM usage_logs;
