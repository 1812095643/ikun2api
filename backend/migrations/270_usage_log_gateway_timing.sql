-- 首字延迟拆分：first_token_ms 只从最后一次转发上游开始计时，不含网关排队与换号重试。
-- 新增网关侧耗时，client_first_token_ms 为用户实际等到首字的时间（转发前耗时 + 上游首字）。
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS gateway_wait_ms INTEGER,
    ADD COLUMN IF NOT EXISTS pre_forward_ms INTEGER,
    ADD COLUMN IF NOT EXISTS account_switch_count INTEGER,
    ADD COLUMN IF NOT EXISTS client_first_token_ms INTEGER;

COMMENT ON COLUMN usage_logs.gateway_wait_ms IS
    'Time spent waiting for user/account concurrency slots inside the gateway (ms)';
COMMENT ON COLUMN usage_logs.pre_forward_ms IS
    'Time from request arrival to the start of the final upstream attempt: body read, queueing, failed attempts (ms)';
COMMENT ON COLUMN usage_logs.account_switch_count IS
    'Number of account switches (failover) before the final upstream attempt';
COMMENT ON COLUMN usage_logs.client_first_token_ms IS
    'Client-perceived time to first token: pre_forward_ms + first_token_ms (ms)';
