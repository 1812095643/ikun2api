-- 实时号池：usage_logs 记录请求时刻的账号名称与平台快照。
-- 历史请求里的账号标签不再随账号改名/换绑/删除而漂移，也避免为展示而回查 accounts 表。
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS account_name_snapshot TEXT,
    ADD COLUMN IF NOT EXISTS account_platform_snapshot TEXT;

COMMENT ON COLUMN usage_logs.account_name_snapshot IS
    'Account name captured when the request was billed; keeps historical usage rows stable';
COMMENT ON COLUMN usage_logs.account_platform_snapshot IS
    'Account platform captured when the request was billed; keeps historical usage rows stable';
