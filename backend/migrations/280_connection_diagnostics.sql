CREATE TABLE IF NOT EXISTS connection_traces (
    started_at TIMESTAMPTZ NOT NULL,
    trace_id UUID NOT NULL,
    request_id VARCHAR(128) NOT NULL DEFAULT '',
    client_request_id VARCHAR(128) NOT NULL DEFAULT '',
    user_id BIGINT NOT NULL DEFAULT 0,
    api_key_id BIGINT NOT NULL DEFAULT 0,
    account_id BIGINT NOT NULL DEFAULT 0,
    group_id BIGINT NOT NULL DEFAULT 0,
    model VARCHAR(256) NOT NULL DEFAULT '',
    path VARCHAR(160) NOT NULL DEFAULT '',
    transport VARCHAR(16) NOT NULL DEFAULT 'http',
    state VARCHAR(40) NOT NULL,
    status INT NOT NULL,
    total_ms DOUBLE PRECISION NOT NULL,
    first_byte_ms DOUBLE PRECISION,
    auth_ms BIGINT,
    routing_ms BIGINT,
    upstream_ms BIGINT,
    first_token_ms BIGINT,
    request_bytes BIGINT NOT NULL DEFAULT 0,
    response_bytes BIGINT NOT NULL DEFAULT 0,
    error_type VARCHAR(96) NOT NULL DEFAULT '',
    detail JSONB,
    PRIMARY KEY (started_at, trace_id)
) PARTITION BY RANGE (started_at);
CREATE INDEX IF NOT EXISTS connection_traces_request_idx ON connection_traces (request_id, started_at DESC, trace_id DESC) WHERE request_id <> '';
CREATE INDEX IF NOT EXISTS connection_traces_client_request_idx ON connection_traces (client_request_id, started_at DESC, trace_id DESC) WHERE client_request_id <> '';
CREATE INDEX IF NOT EXISTS connection_traces_user_idx ON connection_traces (user_id, started_at DESC, trace_id DESC);
CREATE INDEX IF NOT EXISTS connection_traces_account_idx ON connection_traces (account_id, started_at DESC, trace_id DESC);
CREATE INDEX IF NOT EXISTS connection_traces_group_idx ON connection_traces (group_id, started_at DESC, trace_id DESC);
