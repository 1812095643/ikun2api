-- 首字拆分补充：pre_forward_ms 含读完客户端请求体的时间，客户端上传慢时会被误看作网关慢。
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS upload_ms INTEGER;

COMMENT ON COLUMN usage_logs.upload_ms IS
    'Time to finish reading the client request body (client upload), part of pre_forward_ms (ms)';
