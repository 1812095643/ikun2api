CREATE TABLE IF NOT EXISTS workspace_tickets (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_email TEXT NOT NULL DEFAULT '',
    user_name TEXT NOT NULL DEFAULT '',
    subject VARCHAR(300) NOT NULL,
    category TEXT NOT NULL DEFAULT 'general' CHECK (category IN ('general','account','billing','model','bug')),
    priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('low','normal','high','urgent')),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','in_progress','waiting_user','resolved','closed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    user_last_read_at TIMESTAMPTZ,
    admin_last_read_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_workspace_tickets_user ON workspace_tickets(user_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_workspace_tickets_queue ON workspace_tickets(status, created_at, id);

CREATE TABLE IF NOT EXISTS workspace_ticket_messages (
    id BIGSERIAL PRIMARY KEY,
    ticket_id BIGINT NOT NULL REFERENCES workspace_tickets(id) ON DELETE CASCADE,
    author_id BIGINT NOT NULL,
    author_role TEXT NOT NULL CHECK (author_role IN ('user','admin','system')),
    author_name TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT 'message',
    content TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_workspace_ticket_messages_ticket ON workspace_ticket_messages(ticket_id,id);

CREATE TABLE IF NOT EXISTS workspace_ticket_images (
    id TEXT PRIMARY KEY,
    ticket_id BIGINT NOT NULL REFERENCES workspace_tickets(id) ON DELETE CASCADE,
    message_id BIGINT NOT NULL REFERENCES workspace_ticket_messages(id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,
    size_bytes INTEGER NOT NULL,
    mime_type TEXT NOT NULL DEFAULT 'image/jpeg',
    data BYTEA,
    deleted_at TIMESTAMPTZ,
    deleted_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_workspace_ticket_images_ticket ON workspace_ticket_images(ticket_id,message_id);

CREATE TABLE IF NOT EXISTS workspace_relogin_accounts (
    email TEXT PRIMARY KEY,
    secret_blob TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ
);
CREATE TABLE IF NOT EXISTS workspace_relogin_runs (
    id BIGSERIAL PRIMARY KEY,
    email TEXT NOT NULL,
    account_id BIGINT,
    trigger TEXT NOT NULL DEFAULT 'manual',
    status TEXT NOT NULL DEFAULT 'running',
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    error_message TEXT NOT NULL DEFAULT '',
    result_blob TEXT NOT NULL DEFAULT '',
    plan_type TEXT NOT NULL DEFAULT '',
    applied BOOLEAN NOT NULL DEFAULT FALSE,
    apply_action TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_workspace_relogin_runs_email ON workspace_relogin_runs(email,started_at DESC);
CREATE TABLE IF NOT EXISTS workspace_runtime (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS workspace_news_favorites (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    news_id BIGINT NOT NULL,
    item JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id,news_id)
);
