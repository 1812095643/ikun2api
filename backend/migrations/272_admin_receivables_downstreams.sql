-- Administrative credit ledger: monetary values use the same USD unit as user balance.
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_downstream BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX IF NOT EXISTS idx_users_downstream ON users(id) WHERE is_downstream AND deleted_at IS NULL;
ALTER TABLE redeem_codes DROP CONSTRAINT IF EXISTS redeem_codes_credit_source_check;
ALTER TABLE redeem_codes ADD CONSTRAINT redeem_codes_credit_source_check
    CHECK (credit_source IN ('legacy','manual','redeem','gift','lottery','on_account'));
CREATE TABLE IF NOT EXISTS admin_receivable_entries (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    kind TEXT NOT NULL CHECK (kind IN ('credit','payment')),
    amount NUMERIC(20,8) NOT NULL CHECK (amount > 0),
    outstanding_after NUMERIC(20,8) NOT NULL CHECK (outstanding_after >= 0),
    credit_record_id BIGINT UNIQUE REFERENCES redeem_codes(id),
    actor_id BIGINT NOT NULL REFERENCES users(id),
    operation_key UUID NOT NULL UNIQUE,
    notes TEXT NOT NULL DEFAULT '',
    payment_method VARCHAR(100) NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((kind='credit' AND credit_record_id IS NOT NULL) OR (kind='payment' AND credit_record_id IS NULL))
);
CREATE INDEX IF NOT EXISTS idx_receivable_user_created ON admin_receivable_entries(user_id,id DESC);
CREATE TABLE IF NOT EXISTS admin_downstream_operations (
    operation_key UUID PRIMARY KEY,
    actor_id BIGINT NOT NULL REFERENCES users(id),
    payload_digest TEXT NOT NULL,
    affected INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE channel_model_pricing ADD COLUMN IF NOT EXISTS apply_to_downstream BOOLEAN NOT NULL DEFAULT TRUE;
