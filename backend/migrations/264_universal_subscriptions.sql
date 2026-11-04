-- Universal subscriptions are separate financial entitlements. Legacy records remain intact.
ALTER TABLE subscription_plans ALTER COLUMN group_id DROP NOT NULL;
ALTER TABLE subscription_plans ADD COLUMN IF NOT EXISTS plan_type varchar(32) NOT NULL DEFAULT 'group';
ALTER TABLE subscription_plans ADD COLUMN IF NOT EXISTS total_credit_usd numeric(20,8);
ALTER TABLE subscription_plans ADD COLUMN IF NOT EXISTS daily_limit_usd numeric(20,8);
ALTER TABLE subscription_plans ADD COLUMN IF NOT EXISTS weekly_limit_usd numeric(20,8);
ALTER TABLE subscription_plans ADD COLUMN IF NOT EXISTS monthly_limit_usd numeric(20,8);
ALTER TABLE subscription_plans ALTER COLUMN plan_type SET DEFAULT 'universal_credit';
UPDATE subscription_plans SET for_sale=false WHERE plan_type='group' AND for_sale=true;
DO $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='api_keys' AND column_name='funding_source') THEN
  ALTER TABLE api_keys ADD COLUMN funding_source varchar(32) NOT NULL DEFAULT 'balance';
  -- Preserve the financial intent of old subscription Keys; do not silently switch them to wallets.
  IF EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='api_keys' AND column_name='group_id') AND EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='groups' AND column_name='subscription_type') THEN
   UPDATE api_keys k SET funding_source='universal_subscription' FROM groups g WHERE k.group_id=g.id AND g.subscription_type='subscription';
  END IF;
 END IF;
END $$;
ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS subscription_snapshot jsonb;
CREATE TABLE IF NOT EXISTS universal_subscriptions (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id),
 plan_id bigint,
 order_id bigint UNIQUE REFERENCES payment_orders(id),
 idempotency_key varchar(160) NOT NULL UNIQUE,
 name varchar(100) NOT NULL,
 snapshot jsonb NOT NULL,
 starts_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 status varchar(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended','revoked')),
 total_credit_usd numeric(20,8),
 daily_limit_usd numeric(20,8), weekly_limit_usd numeric(20,8), monthly_limit_usd numeric(20,8),
 total_used_usd numeric(20,8) NOT NULL DEFAULT 0 CHECK (total_used_usd>=0),
 daily_used_usd numeric(20,8) NOT NULL DEFAULT 0 CHECK (daily_used_usd>=0),
 weekly_used_usd numeric(20,8) NOT NULL DEFAULT 0 CHECK (weekly_used_usd>=0),
 monthly_used_usd numeric(20,8) NOT NULL DEFAULT 0 CHECK (monthly_used_usd>=0),
 daily_window_start timestamptz NOT NULL,
 weekly_window_start timestamptz NOT NULL,
 monthly_window_start timestamptz NOT NULL,
 assigned_by bigint REFERENCES users(id),
 notes text NOT NULL DEFAULT '',
 version bigint NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK (expires_at>starts_at),
 CHECK (total_credit_usd IS NULL OR total_credit_usd>0),
 CHECK (daily_limit_usd IS NULL OR daily_limit_usd>0),
 CHECK (weekly_limit_usd IS NULL OR weekly_limit_usd>0),
 CHECK (monthly_limit_usd IS NULL OR monthly_limit_usd>0)
);
CREATE INDEX IF NOT EXISTS idx_universal_subscriptions_user ON universal_subscriptions(user_id,status,starts_at,expires_at);
CREATE TABLE IF NOT EXISTS universal_subscription_holds (
 id uuid PRIMARY KEY,
 subscription_id bigint NOT NULL REFERENCES universal_subscriptions(id),
 user_id bigint NOT NULL REFERENCES users(id), api_key_id bigint NOT NULL,
 amount numeric(20,8) NOT NULL CHECK (amount>=0),
 state varchar(20) NOT NULL DEFAULT 'reserved' CHECK (state IN ('reserved','settled','released','unknown')),
 daily_window_start timestamptz NOT NULL, weekly_window_start timestamptz NOT NULL, monthly_window_start timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_universal_holds_pending ON universal_subscription_holds(subscription_id,state);
CREATE TABLE IF NOT EXISTS universal_subscription_charges (
 id bigserial PRIMARY KEY, subscription_id bigint NOT NULL REFERENCES universal_subscriptions(id),
 request_id varchar(256) NOT NULL, api_key_id bigint NOT NULL, hold_id uuid REFERENCES universal_subscription_holds(id),
 amount numeric(20,8) NOT NULL CHECK (amount>=0), created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(request_id,api_key_id)
);
CREATE TABLE IF NOT EXISTS universal_subscription_events (
 id bigserial PRIMARY KEY, subscription_id bigint NOT NULL REFERENCES universal_subscriptions(id),
 action varchar(40) NOT NULL, actor_id bigint, reason text NOT NULL DEFAULT '',
 details jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_universal_events_sub ON universal_subscription_events(subscription_id,id DESC);

ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS subscription_snapshot jsonb;
ALTER TABLE universal_subscriptions ADD COLUMN IF NOT EXISTS refund_pending boolean NOT NULL DEFAULT false;
ALTER TABLE universal_subscriptions ADD COLUMN IF NOT EXISTS refund_previous_status varchar(20);

CREATE TABLE IF NOT EXISTS universal_batch_funding (
 batch_id varchar(128) PRIMARY KEY, user_id bigint NOT NULL REFERENCES users(id), api_key_id bigint NOT NULL,
 source varchar(32) NOT NULL, hold_id uuid REFERENCES universal_subscription_holds(id),
 created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS universal_media_funding (
 task_id varchar(256) NOT NULL,user_id bigint NOT NULL REFERENCES users(id),api_key_id bigint NOT NULL,
 source varchar(32) NOT NULL,hold_id uuid REFERENCES universal_subscription_holds(id),
 created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(task_id,user_id,api_key_id)
);
