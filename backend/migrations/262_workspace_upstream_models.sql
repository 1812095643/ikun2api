-- Model catalog monitoring is opt-in and independent of wallet monitoring.
CREATE TABLE IF NOT EXISTS workspace_upstream_models (
 upstream_id VARCHAR(64) PRIMARY KEY REFERENCES workspace_upstream_balances(id) ON DELETE CASCADE,
 config JSONB NOT NULL DEFAULT '{"enabled":false,"interval_minutes":30,"notify":false,"recipients":[]}',
 state JSONB NOT NULL DEFAULT '[]',
 history JSONB NOT NULL DEFAULT '[]',
 notification JSONB NOT NULL DEFAULT '{}',
 revision BIGINT NOT NULL DEFAULT 1,
 refresh_requested BOOLEAN NOT NULL DEFAULT FALSE,
 requested_at TIMESTAMPTZ,
 lease_until TIMESTAMPTZ,
 last_checked_at TIMESTAMPTZ,
 next_check_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
