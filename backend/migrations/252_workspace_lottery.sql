ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS credit_source TEXT NOT NULL DEFAULT 'legacy'
    CHECK (credit_source IN ('legacy','manual','redeem','gift','lottery'));

CREATE OR REPLACE FUNCTION set_redeem_credit_source() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.credit_source = 'legacy' THEN
        NEW.credit_source := CASE
            WHEN COALESCE(NEW.notes,'') LIKE '[赠送]%' THEN 'gift'
            WHEN NEW.type = 'balance' THEN 'redeem'
            WHEN NEW.type = 'admin_balance' THEN 'manual'
            ELSE 'gift' END;
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS redeem_credit_source ON redeem_codes;
CREATE TRIGGER redeem_credit_source BEFORE INSERT OR UPDATE OF status ON redeem_codes
    FOR EACH ROW EXECUTE FUNCTION set_redeem_credit_source();

CREATE TABLE IF NOT EXISTS workspace_lotteries (
    id BIGSERIAL PRIMARY KEY,
    spec JSONB NOT NULL,
    signature TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','open','drawn','cancelled')),
    created_by BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    drawn_at TIMESTAMPTZ,
    eligible_count INTEGER NOT NULL DEFAULT 0,
    result_digest TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_workspace_lotteries_status ON workspace_lotteries(status,id DESC);

CREATE TABLE IF NOT EXISTS workspace_lottery_entries (
    id BIGSERIAL PRIMARY KEY,
    lottery_id BIGINT NOT NULL REFERENCES workspace_lotteries(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    evidence JSONB NOT NULL,
    signature TEXT NOT NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(lottery_id,user_id)
);
CREATE INDEX IF NOT EXISTS idx_workspace_lottery_entries_user ON workspace_lottery_entries(user_id,id DESC);

CREATE TABLE IF NOT EXISTS workspace_lottery_winners (
    id BIGSERIAL PRIMARY KEY,
    lottery_id BIGINT NOT NULL REFERENCES workspace_lotteries(id),
    entry_id BIGINT NOT NULL UNIQUE REFERENCES workspace_lottery_entries(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    prize_key TEXT NOT NULL,
    amount NUMERIC(20,8) NOT NULL CHECK (amount > 0),
    signature TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at TIMESTAMPTZ,
    redeem_id BIGINT UNIQUE REFERENCES redeem_codes(id),
    balance_after NUMERIC(20,8),
    UNIQUE(lottery_id,user_id),
    CHECK ((claimed_at IS NULL AND redeem_id IS NULL) OR (claimed_at IS NOT NULL AND redeem_id IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS idx_workspace_lottery_winners_user ON workspace_lottery_winners(user_id,id DESC);

CREATE TABLE IF NOT EXISTS workspace_lottery_events (
    id BIGSERIAL PRIMARY KEY,
    lottery_id BIGINT NOT NULL REFERENCES workspace_lotteries(id),
    actor_id BIGINT REFERENCES users(id),
    action TEXT NOT NULL,
    detail JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_workspace_lottery_events_activity ON workspace_lottery_events(lottery_id,id);
