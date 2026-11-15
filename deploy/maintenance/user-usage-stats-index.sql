-- Optional online tuning for the user management usage summary.
-- Run with psql -f, outside a transaction. This does not change billing data.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_user_stats_cover
    ON public.usage_logs (user_id, created_at)
    INCLUDE (actual_cost, group_id, account_id)
    WHERE actual_cost > 0;
