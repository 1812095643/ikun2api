-- Keep the legacy threshold unless a site explicitly changes its credit unit.
ALTER TABLE workspace_blindbox_settings
    ADD COLUMN IF NOT EXISTS usage_per_chance NUMERIC(20,8) NOT NULL DEFAULT 100
    CHECK (usage_per_chance > 0 AND usage_per_chance <= 1000000);
