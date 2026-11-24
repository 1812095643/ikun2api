ALTER TABLE workspace_relogin_runs ADD COLUMN IF NOT EXISTS account_fingerprint TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_workspace_relogin_auto_account_time
ON workspace_relogin_runs(account_id,started_at DESC) WHERE trigger='auto';
