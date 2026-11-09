-- Eligibility, preview and draw checks sum actual_cost by user and business day.
-- Keep the amount in the index so historical checks do not revisit the wide log heap.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_user_created_cost_cover
    ON usage_logs (user_id, created_at) INCLUDE (actual_cost);
