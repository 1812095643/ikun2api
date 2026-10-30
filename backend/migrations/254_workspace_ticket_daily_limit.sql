CREATE TABLE IF NOT EXISTS workspace_ticket_daily_submissions (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    submission_day DATE NOT NULL,
    ticket_id BIGINT REFERENCES workspace_tickets(id) ON DELETE SET NULL,
    PRIMARY KEY (user_id, submission_day)
);

-- 独立保留每日名额，管理员删除工单后也不能重复新建。
-- 上线前当天已有的工单同样占用名额，历史重复工单不作删除。
INSERT INTO workspace_ticket_daily_submissions(user_id, submission_day, ticket_id)
SELECT user_id, (created_at AT TIME ZONE 'Asia/Shanghai')::date, max(id)
FROM workspace_tickets
WHERE created_at >= (CURRENT_TIMESTAMP AT TIME ZONE 'Asia/Shanghai')::date AT TIME ZONE 'Asia/Shanghai'
GROUP BY user_id, (created_at AT TIME ZONE 'Asia/Shanghai')::date
ON CONFLICT (user_id, submission_day) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_workspace_ticket_daily_day ON workspace_ticket_daily_submissions(submission_day);
