
CREATE TABLE IF NOT EXISTS workspace_account_vault (
 id TEXT PRIMARY KEY, platform TEXT NOT NULL, title TEXT NOT NULL DEFAULT '', tags TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL DEFAULT 'unused', account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
 email_hash TEXT NOT NULL, secret_blob TEXT NOT NULL, key_id TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE(platform,email_hash)
);
CREATE TABLE IF NOT EXISTS workspace_vault_batches (
 id TEXT PRIMARY KEY, digest TEXT NOT NULL, result JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS workspace_vault_audit (
 id BIGSERIAL PRIMARY KEY, actor_id BIGINT NOT NULL, record_id TEXT NOT NULL,
 action TEXT NOT NULL, field TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS workspace_vault_audit_created ON workspace_vault_audit(created_at DESC);
CREATE TABLE IF NOT EXISTS workspace_warmup_rules (
 id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, enabled BOOLEAN NOT NULL DEFAULT FALSE,
 priority INTEGER NOT NULL DEFAULT 0, config JSONB NOT NULL, version BIGINT NOT NULL DEFAULT 1, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS workspace_warmup_overrides (
 account_id BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
 mode TEXT NOT NULL CHECK (mode IN ('inherit','disabled','rule')), rule_id BIGINT REFERENCES workspace_warmup_rules(id) ON DELETE SET NULL
);
CREATE TABLE IF NOT EXISTS workspace_warmup_runs (
 id BIGSERIAL PRIMARY KEY, rule_id BIGINT REFERENCES workspace_warmup_rules(id) ON DELETE SET NULL,
 account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE, occurrence TEXT NOT NULL,
 planned_at TIMESTAMPTZ NOT NULL, deadline TIMESTAMPTZ NOT NULL, status TEXT NOT NULL DEFAULT 'queued',
 reason TEXT NOT NULL DEFAULT '', snapshot JSONB NOT NULL DEFAULT '{}', attempts INTEGER NOT NULL DEFAULT 0,
 input_tokens BIGINT NOT NULL DEFAULT 0, output_tokens BIGINT NOT NULL DEFAULT 0,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), started_at TIMESTAMPTZ, finished_at TIMESTAMPTZ, UNIQUE(account_id,occurrence)
);
CREATE INDEX IF NOT EXISTS workspace_warmup_runs_due ON workspace_warmup_runs(planned_at) WHERE status='queued';
CREATE TABLE IF NOT EXISTS workspace_warmup_attempts (
 id BIGSERIAL PRIMARY KEY, run_id BIGINT NOT NULL REFERENCES workspace_warmup_runs(id) ON DELETE CASCADE,
 ordinal INTEGER NOT NULL, status TEXT NOT NULL, summary JSONB NOT NULL DEFAULT '{}',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE(run_id,ordinal)
);
