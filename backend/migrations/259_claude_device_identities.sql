-- Immutable device identity is separate from editable account credentials/extra.
-- Existing Redis IDs are imported by IdentityService before serving requests.
CREATE TABLE IF NOT EXISTS claude_device_identities (
 account_id BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
 device_id VARCHAR(64) NOT NULL CHECK (device_id ~ '^[0-9a-f]{64}$'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
