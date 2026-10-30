CREATE TABLE IF NOT EXISTS workspace_navigation_reads (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    section TEXT NOT NULL CHECK (section IN ('news','reset','models','lottery','admin_lottery','pages')),
    seen_keys JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(seen_keys) = 'array' AND jsonb_array_length(seen_keys) <= 4096),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id,section)
);
