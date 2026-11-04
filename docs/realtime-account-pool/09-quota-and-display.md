# 06 · 额度读数与显示层（5h/7d 真实额度、容量门控、脱敏、列对齐）

> 承接 `01-architecture.md` ~ `05-security-model.md`，记录号池第二轮改造：
> 把「额度」从展示数字做成**可核对的真实读数**，把「账号名」从泄露源做成**脱敏视图**，
> 把「使用记录」对齐管理端 usage 视图的列子集。
>
> 本轮为**纯增量**：账号池原有列（今日请求 / 今日 Tokens / 今日费用 / 最近使用 / 操作）全部保留。

---

## 1. 5 小时 / 7 天额度：数据从哪来

### 1.1 三个数据源，优先级严格

号池不主动探测上游（那会把「看板」变成「压测」），只用已经落库的事实。

| 优先级 | 来源 | 判定键 | `source` | 前端行为 |
|---|---|---|---|---|
| 1 | 上游响应头被动采样 | `accounts.extra.session_window_utilization`（5h）<br>`accounts.extra.passive_usage_7d_utilization`（7d） | `upstream` | 画进度条 + 重置倒计时 |
| 2 | 会话窗口状态 | `accounts.session_window_status` | `status` | `rejected` → 100%，`allowed_warning` → 80% |
| 3 | 本地窗口费用限额 | `capacity.window_cost_limit` | `quota` | 画进度条（仅 anthropic oauth 会真执行） |
| 4 | 只有本地请求量 | `usage_logs` 聚合 | `local` | **只显示次数与金额，不画条、不编百分比** |

### 1.2 被动采样的写入点

`backend/internal/service/ratelimit_service.go` · `samplePassiveUsageFromHeaders(ctx, account, headers)`

| 上游响应头 | 落库字段 | 量纲 |
|---|---|---|
| `anthropic-ratelimit-unified-5h-utilization` | `session_window_utilization` | **0–1 小数** |
| `anthropic-ratelimit-unified-7d-utilization` | `passive_usage_7d_utilization` | 0–1 小数 |
| `anthropic-ratelimit-unified-7d-reset` | `passive_usage_7d_reset` | unix 秒（`> 1e11` 视为毫秒，除 1000） |
| `anthropic-ratelimit-unified-7d_oi-utilization` | `passive_usage_7d_oi_utilization` | 0–1 小数 |
| `anthropic-ratelimit-unified-7d_oi-reset` | `passive_usage_7d_oi_reset` | unix 秒 |
| —（采样成功时间戳） | `passive_usage_sampled_at` | RFC3339 UTC |

**窗口重置时会主动清空**（`ratelimit_service.go` `UpdateExtra` 调用点）：`session_window_utilization` /
`passive_usage_7d_utilization` / `passive_usage_7d_reset` / `passive_usage_7d_oi_utilization` /
`passive_usage_7d_oi_reset` / `passive_usage_sampled_at` 全部置 `nil`。

> 这一步很关键：如果只清 `session_window_*` 不清 `passive_usage_*`，重置后进度条会卡在旧读数，
> 用户会以为「额度一直满着」。清理必须在同一处完成。

### 1.3 5h 窗口的重置点与过期语义

- 重置点取 `accounts.session_window_end`（次选 `rate_limit_reset_at`）。
- **窗口已过期 → 额度确实已经重置**，读数和重置点一起归零：

```go
if acc.SessionWindowEnd != nil && !now.Before(*acc.SessionWindowEnd) {
    window.Utilization = 0
    window.ResetsAt = nil
    window.RemainingSeconds = 0
}
```

这条与官方 `account_usage_service.go` 的 `estimateSetupTokenUsage` 完全一致，
避免号池读数和账号详情页读数打架。

### 1.4 实现位置

`backend/internal/service/account_pool_quota.go`

```go
func poolQuotaWindowFromAccount(acc *Account, kind string, capacity PoolCapacity) PoolQuotaWindow
```

- `kind = poolWindowFiveHour`（`"5h"`）/ `poolWindowSevenDay`（`"7d"`）
- `poolExtraPercent` 做量纲归一：库里 `<= 1` 视为比例并乘 100，`<= 0` 返回 `(0, false)`
- 7d 另有一条 `quota_weekly_*` 换算分支，仅在 `capacity.SupportsQuota` 为真时启用

本地用量回填：`fillWindowStats(ctx, items, ids)` 用 **两次** `GetAccountWindowStatsBatch`：

```go
reader.GetAccountWindowStatsBatch(ctx, ids, now.Add(-5*time.Hour))     // 5h 窗口
reader.GetAccountWindowStatsBatch(ctx, ids, now.AddDate(0, 0, -7))    // 7d 窗口
```

回填 `Requests / Tokens / Cost`；5h 的 `Cost` 同时写入 `Capacity.WindowCostUsed`。
聚合失败时**静默降级**（返回 `local` 读数），不阻断列表。

---

## 2. 容量列与「执行层门控」

### 2.1 两套互斥的限额机制

系统里同时存在两套限额，**各自只对特定账号类型生效**，这是「额度满了还能请求」的根因。

| 机制 | 执行函数 | 生效条件 |
|---|---|---|
| `quota_limit` / `quota_daily_*` / `quota_weekly_*` | `AccountTypeSupportsQuota()` | `api_key` / `bedrock` / `service_account` |
| `window_cost_limit` | `IsAnthropicOAuthOrSetupToken()` | `platform == anthropic` **且** `type ∈ {oauth, setup_token}` |

### 2.2 门控字段

`PoolCapacity` 增加两个布尔，**如实反映「这个限额会不会真被拦」**：

```go
SupportsQuota      bool `json:"supports_quota,omitempty"`       // quota_* 是否真执行
SupportsWindowCost bool `json:"supports_window_cost,omitempty"` // 5h 窗口费用是否真执行
```

前端四个限额徽标（`$窗口费用` / `W 周` / `D 日` / `T 总额`）全部加门控：

```vue
v-if="row.capacity?.supports_window_cost && (row.capacity?.window_cost_limit ?? 0) > 0"
v-if="row.capacity?.supports_quota && (row.capacity?.quota_weekly_limit ?? 0) > 0"
```

**设计取舍**：显示一个不执行的限额，比不显示更危险 —— 管理员会以为号被限死了，
实际请求还在往里灌。所以「不执行就不显示」。

已知副作用：`platform=claude` 的 oauth 账号 `supports_window_cost=false`（`IsAnthropicOAuthOrSetupToken`
要求 `anthropic`）。真实 ClaudeMax 号池账号 platform 应为 `anthropic`，届时徽标自然出现。

### 2.3 配额体检：`quota_warning`

```go
func poolQuotaWarning(acc *Account, item PoolAccount) string
```

只在 `item.Schedulable == true` 时计算（不可调度的账号没有"还在被调度"这个问题）：

| 返回值 | 触发条件 |
|---|---|
| `quota_exceeded_but_schedulable` | `acc.SupportsAccountQuota() && acc.IsQuotaExceeded()` |
| `window_cost_exceeded_but_schedulable` | anthropic oauth 且 `WindowCostUsed >= WindowCostLimit > 0` |
| `window_rejected_but_schedulable` | `session_window_status == "rejected"` |
| `window_exhausted_but_schedulable` | `session_window_utilization >= 100` |

四条按上表顺序短路，命中即返回。前端在 `#cell-capacity` 渲染红色警示 chip，
`title` 走 i18n：`admin.pool.extra.quotaWarning.<key>`。

> 这不是把 bug 修掉，而是**把不一致显式暴露**。调度层为什么还会选它、要不要改调度，
> 属于独立决策；号池的职责是「不让你在不知情的情况下以为额度还在」。

---

## 3. 账号名脱敏与伪名

### 3.1 规则

| 账号类型 | 用户看到 | 实现 |
|---|---|---|
| `type == oauth`（自有真实号） | `csd******@outlook.com` | `PoolMaskAccountName` |
| 其他（api_key 等） | `amy@outlook.com` 之类的伪名 | `poolPseudoAccountName` |

`poolMaskToken` 保位规则：`n<=1` 保 0 位，`n==2` 保 1 位，`n<=5` 保 2 位，否则保 3 位，后接固定 6 个 `*`。

### 3.2 伪名稳定性（「一段时间内都在一个账号」）

```go
bucket := at.Unix() / poolPseudoRotationSeconds   // 7 * 24 * 3600
seed   := fmt.Sprintf("%d:%d:%d", userID, accountID, bucket)
sum    := sha256.Sum256([]byte(poolPseudoNameSalt + "|" + seed))
local  := poolFakeLocalParts[int(sum[0]) % len(poolFakeLocalParts)]   // 32 个候选
domain := poolFakeDomains[int(sum[1]) % len(poolFakeDomains)]         // 5 个候选
```

- 同一 `(userID, accountID)` 在同一个 7 天桶内名字恒定 → 历史流水看起来是同一个号
- 跨桶自动换名 → 满足「可以隔段时间换账号」
- 纯函数，无状态，多实例部署结果一致

### 3.3 使用记录里的账号名也必须脱敏

**这是一个真实漏洞**：`dto.UsageLogFromService` → `usageLogFromServiceUser` 原本直接下发
`AccountNameSnapshot`（真实账号名快照）。号池列表脱敏了，但使用记录把它全漏出去了。

修复链路：

```go
// account_pool_quota.go
func (s *AccountPoolService) PoolUsageAccountNames(ctx, userID int64, accountIDs []int64) map[int64]string
```

- 用 `adminService.GetAccountsByIDs` 批量取账号（一次查询，避免 N+1）
- 逐条走 `poolDisplayAccountNameForUser`
- **账号已删除**时用 `poolPseudoAccountName(userID, id, now)` 兜底 —— 绝不回退到快照值

```go
// handler/pool_handler.go
func (h *PoolHandler) maskUsageAccountNames(ctx context.Context, userID int64, items []*dto.UsageLog)
```

在 `Usage` 与 `AccountRequests` 两处返回前调用。

> 注意 `dto.UsageLog.AccountID` 是 `int64` 非指针，用 `> 0` 判断有效性。
> 管理员路径（`AdminUsageLog`）**不改**，管理员本来就该看真名。

---

## 4. 分组筛选

- 后端：`GET /admin/pool/accounts?group_id=N`，非法值 `400 Invalid group_id`
- 前端：`accountFilters.group_id`，下拉数据来自 `@/api/admin/groups` 的 `getAll()`（`/admin/groups/all`）
- **仅管理员可见**（`v-if="isAdmin"`）
- 加载失败降级为只含「全部分组」一项，不阻断列表

> 踩坑：一开始用了 `@/api/groups` 的 `getAvailable()`，它打的是用户端 `/groups/available`，
> 只返回「当前用户可绑定的分组」，管理员看不全。必须走 admin 侧接口。

---

## 5. 前端列定义

### 5.1 账号池

管理员：

```
账号 · 平台/类型 · 容量 · 状态 · 分组 · 用量窗口 ·
今日请求 · 今日 Tokens · 今日费用 ·
倍率 · 优先级 · 可用性 · 最近使用 · 操作
```

普通用户（去掉管理员专属列）：

```
账号 · 平台/类型 · 容量 · 状态 · 分组 · 用量窗口 ·
今日请求 · 今日 Tokens · 今日费用 · 最近使用 · 操作
```

- `concurrency` 列已并入 `capacity`（显示 `in_use / concurrency`），不再单独占列
- 账号名去掉 `#ID` 副行；管理员仍可在副行看到 `notes`
- `#cell-usage_window` 渲染 5h / 7d 两条进度条，`source=local` 时显示 `—`

### 5.2 使用记录

管理员：

```
用户 · 账号 · 模型 · 推理强度 · 端点 · 分组 · 类型 ·
输入/输出 · 费用 · 耗时 · 时间 · User-Agent · IP
```

普通用户：去掉「用户」列（数据本来就只属于自己）。

`类型` 由 `row.stream` 决定（流式 / 非流式）；`分组` 取 `row.group?.name`。

> 底层字段全部已存在（`usage_logs.user_agent` / `ip_address` / `group_id` / `reasoning_effort`），
> **不需要迁移**。`user_agent` / `ip_address` 在历史数据里是 NULL，前端显示 `—`。

---

## 6. 验证记录

镜像 `1.3.39-pool9`（`3ea7d5c295d5`），部署于 `43.131.251.155` `/root/ikun2api`。

### 6.1 额度读数

向 `#3` 注入 `extra.session_window_utilization=0.62`、`passive_usage_7d_utilization=0.44`、
`passive_usage_7d_reset=now+3d`、`session_window_end=now+2h`：

```
5h: util=62 src=upstream resets=2026-11-04T22:51:08+08:00 rem=7199
7d: util=44 src=upstream resets=2026-11-07T20:51:08+08:00
```

### 6.2 告警四场景

| 注入 | 输出 |
|---|---|
| `session_window_status='rejected'` | `window_rejected_but_schedulable`，5h `src=status util=100` |
| `session_window_utilization=1.0` | `window_exhausted_but_schedulable`，`util=100` |
| `session_window_end` 已过期 | `util=0`，`resets_at` 为 null，**无告警** |
| `schedulable=false` | **无告警** |

### 6.3 脱敏

- 管理员：`POOL-TEST-OAI-A`
- demo1：账号池 `POO******`；使用记录只返回自己的 2 条，账号名同样 `POO******`

### 6.4 越权

- demo1 访问 `/admin/pool` → 前端守卫跳 `/dashboard`；直连 `/api/v1/admin/pool/*` → `403 Admin access required`
- 侧边栏普通用户只有 `/pool`

### 6.5 分组

- `group_id=1` → `total=0`（该分组下无账号）
- `group_id=abc` → `400 Invalid group_id`

---

## 7. 已知限制

1. **5h 读数依赖上游响应头**。账号长期不产生流量 → `session_window_utilization` 不更新，
   进度条会停在最后一次采样值。是否要按 `passive_usage_sampled_at` 显示「数据陈旧」标记，待定。
2. **`source=local` 的窗口没有百分比**。这是刻意设计：只有本地请求数不足以推算额度占比，
   编一个数字比留空更糟。
3. **`window_cost_limit` 只对 anthropic oauth 生效**。其他平台配了也不拦，前端已按此隐藏徽标。
4. **使用记录的时间区间由前端决定**：默认「今天往前一天」的日期粒度，`toRFC3339` 展开为
   `T00:00:00.000` / `T23:59:59.999` 本地时间再转 ISO。
   > 验收提示：往 `[role=tab]` 上打合成 `click()` 有时不会触发 `fetchUsage`（`performance` 里连请求都没有），
   > 会被误判成「使用记录为空」。请用整页导航 `?tab=usage` 或硬刷新验收。
5. **合成账号（多少个随机号 / 多少封禁）尚未实现**。当前脱敏只作用于真实账号。
