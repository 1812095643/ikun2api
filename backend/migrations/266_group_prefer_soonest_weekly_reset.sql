-- Existing Claude groups and new groups prefer eligible accounts whose weekly
-- quota resets soonest. Other platforms ignore this scheduling preference.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS prefer_soonest_weekly_reset BOOLEAN NOT NULL DEFAULT TRUE;

COMMENT ON COLUMN groups.prefer_soonest_weekly_reset IS
    'Prefer the soonest future weekly quota reset for eligible Claude accounts';
