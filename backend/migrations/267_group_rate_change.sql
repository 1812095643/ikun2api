-- Keep only the most recent actual base-rate change, including direct SQL edits.
-- Existing groups have no historical baseline; do not infer one during upgrade.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS previous_rate_multiplier DECIMAL(10,4),
    ADD COLUMN IF NOT EXISTS rate_multiplier_changed_at TIMESTAMPTZ;

CREATE OR REPLACE FUNCTION track_group_rate_change() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.rate_multiplier IS DISTINCT FROM OLD.rate_multiplier THEN
        NEW.previous_rate_multiplier := OLD.rate_multiplier;
        NEW.rate_multiplier_changed_at := clock_timestamp();
    ELSE
        NEW.previous_rate_multiplier := OLD.previous_rate_multiplier;
        NEW.rate_multiplier_changed_at := OLD.rate_multiplier_changed_at;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS groups_rate_change ON groups;
CREATE TRIGGER groups_rate_change
    BEFORE UPDATE ON groups
    FOR EACH ROW EXECUTE FUNCTION track_group_rate_change();
