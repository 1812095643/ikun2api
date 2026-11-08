-- Ordered key-level routing, atomic with the legacy primary group.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS route_group_ids JSONB;
CREATE INDEX IF NOT EXISTS idx_api_keys_route_group_ids ON api_keys USING GIN (route_group_ids);
COMMENT ON COLUMN api_keys.route_group_ids IS 'Ordered smart-routing candidate groups; empty uses group_id';
