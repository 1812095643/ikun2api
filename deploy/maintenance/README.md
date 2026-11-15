# 用户用量统计查询优化

用户管理的用量汇总需要按用户、时间读取实付费用，并通过分组、账号识别平台。数据量较大的 PostgreSQL 站点可独立执行 `user-usage-stats-index.sql`，利用覆盖索引减少读取完整日志行。

使用 `psql -f` 执行，保持自动提交；`CREATE INDEX CONCURRENTLY` 允许业务继续写入，不能放进显式事务。若同名索引已经存在，先核对定义及 `pg_index.indisvalid`，不要把 `IF NOT EXISTS` 当作有效性校验。

大批量更新历史数据后，可另行执行 `VACUUM (ANALYZE) public.usage_logs`，刷新统计信息和可见性映射。该操作采用常规 VACUUM；无需停站或重写业务表。

此脚本为独立运维选项，不修改迁移校验记录，也不更改费用或余额。
