CREATE TABLE IF NOT EXISTS workspace_checkin_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    mode TEXT NOT NULL DEFAULT 'fixed' CHECK (mode IN ('fixed','random')),
    fixed_amount NUMERIC(20,8) NOT NULL DEFAULT 0.1 CHECK (fixed_amount > 0 AND fixed_amount <= 1000000),
    random_min NUMERIC(20,8) NOT NULL DEFAULT 0.01 CHECK (random_min > 0),
    random_max NUMERIC(20,8) NOT NULL DEFAULT 0.1 CHECK (random_max >= random_min AND random_max <= 1000000),
    updated_by BIGINT REFERENCES users(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO workspace_checkin_settings(id) VALUES(1) ON CONFLICT(id) DO NOTHING;

CREATE TABLE IF NOT EXISTS workspace_checkins (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    checkin_day DATE NOT NULL,
    amount NUMERIC(20,8) NOT NULL CHECK (amount > 0 AND amount <= 1000000),
    consecutive_days INTEGER NOT NULL CHECK (consecutive_days > 0),
    reward_mode TEXT NOT NULL CHECK (reward_mode IN ('fixed','random')),
    reward_min NUMERIC(20,8) NOT NULL,
    reward_max NUMERIC(20,8) NOT NULL,
    redeem_id BIGINT NOT NULL UNIQUE REFERENCES redeem_codes(id),
    balance_after NUMERIC(20,8) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id,checkin_day),
    CHECK (reward_min > 0 AND reward_max >= reward_min AND amount BETWEEN reward_min AND reward_max),
    CHECK (reward_mode <> 'fixed' OR reward_min = reward_max)
);
