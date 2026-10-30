-- IKUN: aggregate API-key upstreams without modifying account credentials or routing.
CREATE TABLE IF NOT EXISTS workspace_upstream_balances (
    id VARCHAR(64) PRIMARY KEY,
    url TEXT NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    config JSONB NOT NULL DEFAULT '{}',
    secret_blob TEXT NOT NULL DEFAULT '',
    source_hash VARCHAR(64) NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    snapshot JSONB NOT NULL DEFAULT '{}',
    alert JSONB NOT NULL DEFAULT '{}',
    refresh_requested BOOLEAN NOT NULL DEFAULT FALSE,
    requested_at TIMESTAMPTZ,
    lease_until TIMESTAMPTZ,
    last_checked_at TIMESTAMPTZ,
    next_check_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
