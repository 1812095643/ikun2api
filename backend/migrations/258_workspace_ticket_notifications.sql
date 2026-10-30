ALTER TABLE workspace_tickets ADD COLUMN origin TEXT NOT NULL DEFAULT 'user'
    CHECK (origin IN ('user','admin'));

CREATE TABLE workspace_ticket_notifications (
    admin_id BIGINT NOT NULL REFERENCES users(id),
    request_key TEXT NOT NULL CHECK (length(request_key) BETWEEN 16 AND 64),
    user_id BIGINT NOT NULL REFERENCES users(id),
    payload_hash TEXT NOT NULL,
    ticket_id BIGINT REFERENCES workspace_tickets(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (admin_id,request_key)
);
