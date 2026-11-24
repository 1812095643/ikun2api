-- Admin notification display names only. Never include API keys or credentials.
CREATE OR REPLACE VIEW ops_monitor_v1_identity AS
SELECT 'user'::text AS kind, id, 0::bigint AS owner_id, COALESCE(username,'')::text AS name, COALESCE(email,'')::text AS email FROM users WHERE deleted_at IS NULL
UNION ALL SELECT 'key', id, user_id, COALESCE(name,'')::text, ''::text FROM api_keys WHERE deleted_at IS NULL
UNION ALL SELECT 'account', id, 0::bigint, COALESCE(name,'')::text, ''::text FROM accounts WHERE deleted_at IS NULL
UNION ALL SELECT 'group', id, 0::bigint, COALESCE(name,'')::text, ''::text FROM groups WHERE deleted_at IS NULL;
