# 数据模型与迁移

## 1. 新增列：`usage_logs` 的两个快照字段

### 迁移脚本 `backend/migrations/263_usage_log_account_snapshot.sql`

```sql
-- 实时号池：usage_logs 记录请求时刻的账号名称与平台快照。
-- 历史请求里的账号标签不再随账号改名/换绑/删除而漂移，也避免为展示而回查 accounts 表。
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS account_name_snapshot TEXT,
    ADD COLUMN IF NOT EXISTS account_platform_snapshot TEXT;

COMMENT ON COLUMN usage_logs.account_name_snapshot IS
    'Account name captured when the request was billed; keeps historical usage rows stable';
COMMENT ON COLUMN usage_logs.account_platform_snapshot IS
    'Account platform captured when the request was billed; keeps historical usage rows stable';
```

两列都可 NULL、无默认值、无索引。原因：

- **可 NULL**：加列时已有几十万历史行，填默认值会锁表。历史行保持 NULL 语义正确
  （「当时没记录」），展示层按「有就显示，没有就退化」处理。
- **无索引**：没有任何查询按这两列过滤，只作为 SELECT 输出列。
- **IF NOT EXISTS**：迁移可以重复执行。

### 为什么用快照而不是 join

账号名会变：运营改名（`POOL-TEST-OAI-A` → `OpenAI-主力-01`）、账号被删、账号被换绑。
如果使用记录页实时 join `accounts` 表：

| 场景 | join 的结果 | 快照的结果 |
|---|---|---|
| 账号改名 | 历史记录全部显示新名字，看起来像串改 | 历史记录保留当时的名字 |
| 账号删除 | `NULL` → 页面显示「账号已删除」 | 仍能显示当时的名字 |
| 账号换绑到别的平台 | 历史记录显示新平台 | 保留当时的平台 |

所以快照是**正确性需求**，不是性能优化。顺带省掉了 join。

## 2. 写入点

`service/usage_log_account_snapshot.go` 是唯一的取值入口：

```go
// accountNameSnapshot 返回写入 usage_logs.account_name_snapshot 的账号名称快照。
// 快照在计费时刻冻结账号标识，历史用量记录不会因为账号改名、换绑或删除而漂移。
// 账号为空或名称空白时返回 nil，落库为 NULL。
func accountNameSnapshot(account *Account) *string {
	if account == nil {
		return nil
	}
	return optionalTrimmedStringPtr(account.Name)
}

func accountPlatformSnapshot(account *Account) *string {
	if account == nil {
		return nil
	}
	return optionalTrimmedStringPtr(account.Platform)
}

// accountSnapshotLabel 把快照列还原成可展示的账号标签：优先用名称快照，
// 缺失时退回平台快照，都没有则返回空串，绝不回查 accounts 表。
func accountSnapshotLabel(name *string, platform *string) string {
	if name != nil {
		if trimmed := strings.TrimSpace(*name); trimmed != "" {
			return trimmed
		}
	}
	if platform != nil {
		return strings.TrimSpace(*platform)
	}
	return ""
}
```

两条计费路径各调一次：

- `backend/internal/service/gateway_usage_billing.go:1405-1406`（Anthropic / 通用网关）
- `backend/internal/service/openai_gateway_usage.go:392-393`（OpenAI 网关）

```go
AccountNameSnapshot:     accountNameSnapshot(account),
AccountPlatformSnapshot: accountPlatformSnapshot(account),
```

**批量插入**也必须带上这两列，否则批量路径写进去是 NULL：
`backend/internal/repository/usage_log_repo_insert.go` 的 `INSERT INTO usage_logs (...)` 列表 +2 列，
`usage_log_repo_query.go` 的 SELECT 列表 +2 列，对应 scan 目标 +2 个 `&log.AccountNameSnapshot, &log.AccountPlatformSnapshot`。

## 3. 读取链路（三条，都要通）

```
DB 两列
 ├─► service.UsageLog        （usage_log.go:175-179，两个 *string 字段）
 │    ├─► dto.AdminUsageLog  （types.go:723-726，omitempty）
 │    │     ← mappers.go:789-790 UsageLogFromServiceAdmin 赋值
 │    └─► dto.UsageLog       （types.go:623-626，只加 name，不加 platform）
 │          ← mappers.go:710 usageLogFromServiceUser 赋值
 └─► usagestats 聚合（不读快照，只读金额/用量列）
```

### 用户端只放 name 不放 platform（刻意的）

`usageLogFromServiceUser` 上方有一句注释，是这条边界的守则：

```go
// 普通用户 DTO：严禁包含管理员字段（例如 account_rate_multiplier、account、upstream_model）
```

账号**平台**在用户视角属于调度内部信息（用户能通过平台分布反推池子结构），
所以 `dto.UsageLog` 只加 `AccountNameSnapshot` 一个字段。前端 `accountLabel(row)`
优先读它，读不到才退化到 `row.account?.name`，最后才是「账号已删除」。

## 4. 费用口径（本功能最容易错的地方）

`usage_logs` 里跟钱有关的列有四个：

| 列 | 含义 |
|---|---|
| `total_cost` | 平台标准价（**不含**账号倍率） |
| `actual_cost` | 用户实付（已含用户侧倍率） |
| `account_stats_cost` | 账号侧成本（**不含**账号倍率） |
| `account_rate_multiplier` | 账号倍率（例如 Claude 账号 2.0×） |

「账号侧真实成本」的正确算式是：

```sql
COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)
```

### 踩过的 bug（v1.3.39-pool5）

逐账号卡片用的是正确的乘倍率口径，但概览用的是 `GetGlobalStats`，它的 SQL 是：

```sql
COALESCE(SUM(account_stats_cost), 0) as total_account_cost  -- ← 裸 SUM，漏了倍率
```

结果概览 `today.cost = 0.0440`，三张卡加起来 `0.062`，差 `0.018`
（正好等于 `POOL-TEST-CC-A` 的 `rate_multiplier=2` 应当多出的部分）。
运营第一反应会是"系统算错钱了"。

**修法**：新增 `GetPoolGlobalWindowStats`，SQL 与逐账号查询**逐字对齐**：

```sql
SELECT COUNT(*) as requests,
       COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0) as tokens,
       COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) as cost,
       COALESCE(SUM(total_cost), 0) as standard_cost,
       COALESCE(SUM(actual_cost), 0) as user_cost
FROM usage_logs
WHERE created_at >= $1
```

并在 `account_pool_service.go` 里**明确拒绝**用 `GetGlobalStats` 兜底：

```go
func (s *AccountPoolService) poolGlobalWindowStats(ctx context.Context, startTime time.Time) *usagestats.AccountStats {
	reader, ok := s.usageLogRepo.(poolGlobalStatsReader)
	if !ok {
		// 故意不 fallback 到 GetGlobalStats：那个方法返回的 total_account_cost
		// 是裸 SUM(account_stats_cost)，不含账号倍率，与逐账号口径不一致。
		return nil
	}
	...
}
```

### 口径自检（每次改这块都要跑）

```
概览 today.cost  ==  Σ 各账号卡片 today.cost
概览 today.tokens == Σ 各账号卡片 today.tokens
概览 today.requests == Σ 各账号卡片 today.requests
```

用户端同理，且用户端 `cost` 应等于 `Σ user_cost`（用户实付，不看账号倍率）。

## 5. 用户端的两条查询

### `ListAccountIDsByUser`

```sql
SELECT DISTINCT account_id
FROM usage_logs
WHERE user_id = $1 AND created_at >= $2 AND account_id > 0
ORDER BY account_id
```

`account_id > 0` 排除历史脏数据（早期未记录账号的行是 0）。

### `GetUserAccountWindowStatsBatch`

```sql
SELECT account_id,
       COUNT(*) as requests,
       COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0) as tokens,
       COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) as cost,
       COALESCE(SUM(total_cost), 0) as standard_cost,
       COALESCE(SUM(actual_cost), 0) as user_cost
FROM usage_logs
WHERE user_id = $1 AND account_id = ANY($2) AND created_at >= $3
GROUP BY account_id
```

**两个条件缺一不可**：`user_id` 限定本人，`account_id = ANY(...)` 限定本次要查的账号。
只有 `account_id` 会把别的用户在同一账号上的消耗算进来。

返回后用 `pq.Array(accountIDs)` 传数组，末尾把查询没命中的账号补成
`&usagestats.AccountStats{}`，保证调用方拿到的是完整 map，不用判空。

## 6. 表结构速查

`accounts` 表与本功能相关的列：

```
id, name, platform, type, status, schedulable,
concurrency, priority, rate_multiplier, load_factor,
last_used_at, expires_at,
rate_limited_at, rate_limit_reset_at, overload_until,
session_window_start, session_window_end, session_window_status,
temp_unschedulable_until, temp_unschedulable_reason,
notes, error_message, parent_account_id, quota_dimension
```

`usage_logs` 表与本功能相关的列：

```
id, user_id, api_key_id, account_id, request_id,
model, upstream_model, inbound_endpoint,
input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
total_cost, actual_cost,
account_stats_cost, account_rate_multiplier,
account_name_snapshot, account_platform_snapshot,   ← 本次新增
duration_ms, stream, created_at
```