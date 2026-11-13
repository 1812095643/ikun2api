-- Account tier capabilities/prices live in accounts.extra. Existing accounts keep defaults.
ALTER TABLE groups ADD COLUMN IF NOT EXISTS force_openai_ultrafast BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS forced_service_tier VARCHAR(16) NOT NULL DEFAULT '';
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS service_tier_multiplier DECIMAL(12,4);
ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS api_keys_forced_service_tier_check;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_forced_service_tier_check CHECK (forced_service_tier IN ('', 'priority', 'ultrafast'));
ALTER TABLE groups DROP CONSTRAINT IF EXISTS groups_openai_forced_tier_check;
ALTER TABLE groups ADD CONSTRAINT groups_openai_forced_tier_check CHECK (NOT (force_openai_fast AND force_openai_ultrafast));
