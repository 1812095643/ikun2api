CREATE TABLE IF NOT EXISTS workspace_blindbox_settings (
    id INTEGER PRIMARY KEY CHECK (id=1),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    price NUMERIC(20,8) NOT NULL DEFAULT 2.5,
    minimum NUMERIC(20,8) NOT NULL DEFAULT 1,
    maximum NUMERIC(20,8) NOT NULL DEFAULT 10,
    draw_time TEXT NOT NULL DEFAULT '21:00',
    updated_by BIGINT REFERENCES users(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (minimum >= 0.01 AND minimum < price AND price < maximum AND maximum <= 1000),
    CHECK (price=ROUND(price,2) AND minimum=ROUND(minimum,2) AND maximum=ROUND(maximum,2)),
    CHECK (draw_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$')
);
INSERT INTO workspace_blindbox_settings(id) VALUES(1) ON CONFLICT(id) DO NOTHING;

CREATE TABLE IF NOT EXISTS workspace_blindbox_rounds (
    id BIGSERIAL PRIMARY KEY,
    business_date DATE NOT NULL UNIQUE,
    spec JSONB NOT NULL,
    signature TEXT NOT NULL,
    draw_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','settled')),
    entry_count INTEGER NOT NULL DEFAULT 0 CHECK (entry_count BETWEEN 0 AND 10000),
    total_paid NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (total_paid >= 0),
    total_awarded NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (total_awarded >= 0),
    result_digest TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settled_at TIMESTAMPTZ,
    CHECK ((status='open' AND settled_at IS NULL AND total_awarded=0)
        OR (status='settled' AND settled_at IS NOT NULL AND total_awarded=total_paid))
);
CREATE INDEX IF NOT EXISTS idx_blindbox_rounds_due ON workspace_blindbox_rounds(draw_at) WHERE status='open';

CREATE TABLE IF NOT EXISTS workspace_blindbox_entries (
    id BIGSERIAL PRIMARY KEY,
    round_id BIGINT NOT NULL REFERENCES workspace_blindbox_rounds(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    request_key TEXT NOT NULL CHECK (length(request_key) BETWEEN 16 AND 64),
    paid NUMERIC(20,8) NOT NULL CHECK (paid > 0),
    usage_amount NUMERIC(20,10) NOT NULL CHECK (usage_amount > 0),
    ordinal INTEGER NOT NULL CHECK (ordinal > 0),
    debit_id BIGINT NOT NULL UNIQUE REFERENCES redeem_codes(id),
    signature TEXT NOT NULL,
    reward NUMERIC(20,8) CHECK (reward > 0),
    reward_signature TEXT,
    credit_id BIGINT UNIQUE REFERENCES redeem_codes(id),
    opened_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id,request_key),
    UNIQUE(round_id,user_id,ordinal),
    CHECK ((reward IS NULL AND credit_id IS NULL AND reward_signature IS NULL AND opened_at IS NULL)
        OR (reward IS NOT NULL AND credit_id IS NOT NULL AND reward_signature IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS idx_blindbox_entries_round ON workspace_blindbox_entries(round_id,id);
CREATE INDEX IF NOT EXISTS idx_blindbox_entries_user ON workspace_blindbox_entries(user_id,id DESC);
