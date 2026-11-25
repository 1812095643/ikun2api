-- Run as database owner. Provision ops_monitor LOGIN password separately via a protected channel.
-- Never grant this role membership in application roles or SELECT on all tables.
DO $$ BEGIN IF NOT EXISTS(SELECT FROM pg_roles WHERE rolname='ops_monitor') THEN CREATE ROLE ops_monitor NOLOGIN; END IF; END $$;
GRANT USAGE ON SCHEMA public TO ops_monitor;
GRANT SELECT ON ops_monitor_v1_errors,ops_monitor_v1_attempts,ops_monitor_v1_usage,ops_monitor_v1_accounts,
 ops_monitor_v1_balances,ops_monitor_v1_subscriptions,ops_monitor_v1_payments,
 ops_monitor_v1_holds,ops_monitor_v1_system,ops_monitor_v1_billing,ops_monitor_v1_reconciliation TO ops_monitor;
ALTER ROLE ops_monitor SET default_transaction_read_only=on;
ALTER ROLE ops_monitor SET statement_timeout='3s';
ALTER ROLE ops_monitor SET idle_in_transaction_session_timeout='5s';
