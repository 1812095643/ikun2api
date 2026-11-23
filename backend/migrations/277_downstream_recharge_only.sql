ALTER TABLE users ADD COLUMN IF NOT EXISTS downstream_recharge_only boolean NOT NULL DEFAULT false;
