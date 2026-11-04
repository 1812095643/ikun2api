# 后端 API 契约

所有接口统一返回 `{ "code": 0, "message": "success", "data": ... }`。
分页接口的 `data` 是 `{ "items": [...], "total": N, "page": 1, "page_size": 20, "pages": M }`。

鉴权：`Authorization: Bearer <jwt>`。

---

## A 组：管理端 `/api/v1/admin/pool/*`

整个 `/admin` 组挂了 `AdminAuthMiddleware`，非 admin 一律 `403 FORBIDDEN / Admin access required`。
另外还有一道 `AdminComplianceGuard`：管理员首次访问时返回 `423`，需先接受合规承诺，
见本文末尾。

### A1 `GET /api/v1/admin/pool/overview`

**入参**：无。

**出参** `PoolOverview`：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "total_accounts": 3,
    "active_accounts": 2,
    "error_accounts": 1,
    "disabled_accounts": 0,
    "schedulable_count": 2,
    "platform_counts": { "claude": 1, "openai": 2 },
    "total_in_use": 0,
    "today": {
      "requests": 3, "tokens": 6300,
      "cost": 0.062, "standard_cost": 0.044, "user_cost": 0.044
    },
    "window_7d": {
      "requests": 8, "tokens": 19500,
      "cost": 0.2063, "standard_cost": 0.1269, "user_cost": 0.1269
    },
    "generated_at": "2026-11-04T10:02:52+08:00"
  }
}
```

字段语义：

| 字段 | 来源 | 说明 |
|---|---|---|
| `total_accounts` | `COUNT(accounts)` | 未软删的账号数 |
| `active_accounts` | `status='active'` | |
| `error_accounts` | `status='error'` | |
| `disabled_accounts` | `status='disabled'` | |
| `schedulable_count` | `schedulable=true` 且未过期 | 真正能被调度到的数量 |
| `platform_counts` | `GROUP BY platform` | |
| `total_in_use` | 并发服务的实时计数 | 当前被请求占用的账号数 |
| `today.cost` | 见 `02-data-model.md` 口径 | **含账号倍率** |
| `today.standard_cost` | `SUM(total_cost)` | 平台标准价 |
| `today.user_cost` | `SUM(actual_cost)` | 用户实付 |
| `generated_at` | `timezone.Now()` | 前端可显示"数据更新于…" |

### A2 `GET /api/v1/admin/pool/accounts`

**入参**（query）：

| 参数 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `page` | int | 1 | ≤0 会被修正为 1 |
| `page_size` | int | 20 | ≤0→20，>200→200 |
| `platform` | string | - | 精确匹配 |
| `type` | string | - | `oauth` / `apikey` / … |
| `status` | string | - | `active` / `error` / `disabled` |
| `search` | string | - | 匹配 `name`（模糊） |
| `group_id` | int64 | - | 按分组过滤 |

**出参** `PoolAccountList`，每项 `PoolAccount`：

```json
{
  "id": 1,
  "name": "POOL-TEST-OAI-A",
  "platform": "openai",
  "type": "oauth",
  "status": "active",
  "notes": "号池验收账号A",
  "schedulable": true,
  "error_message": "",
  "last_used_at": "2026-11-04T14:41:20+08:00",
  "rate_multiplier": 1,
  "priority": 0,
  "concurrency": 5,
  "load_factor": 1,
  "in_use": 0,
  "rate_limited_at": null,
  "rate_limit_reset_at": null,
  "overload_until": null,
  "temp_unschedulable_until": null,
  "temp_unschedulable_reason": null,
  "session_window_start": null,
  "session_window_end": null,
  "session_window_status": "",
  "parent_account_id": null,
  "groups": [{ "id": 1, "name": "默认分组" }],
  "today": { "requests": 2, "tokens": 3500, "cost": 0.026, "standard_cost": 0.026, "user_cost": 0.026 }
}
```

数据来源拼装：

1. `adminService.ListAccounts(...)` → 基础字段 + `groups`
2. `fillTodayStats` → `today`
3. `fillConcurrency` → `in_use`
4. `poolRateMultiplier` / `poolLoadFactor` → 空指针归一化为 `1`
5. `poolExpired` → `schedulable` 与 `expires_at` 联合判定

### A3 `GET /api/v1/admin/pool/accounts/:id/requests`

某个账号上的近期请求流水（**不限用户**，管理端视角）。

**入参**：

| 参数 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `page` / `page_size` | int | 1 / 20 | |
| `start_time` | RFC3339 | now-24h | |
| `end_time` | RFC3339 | now | |
| `model` | string | - | |

**出参**：`items` 每项是 `dto.AdminUsageLog`（含 `account`、`account_name_snapshot`、
`account_platform_snapshot`、`upstream_model`、`account_stats_cost`、`ip_address` 等管理端字段）。

### A4 `GET /api/v1/admin/pool/usage`

全站号池使用记录。

**入参**：

| 参数 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `page` / `page_size` | int | 1 / 20 | |
| `start_time` / `end_time` | RFC3339 | now-24h / now | |
| `user_id` | int64 | - | |
| `account_id` | int64 | - | |
| `api_key_id` | int64 | - | |
| `model` | string | - | |
| `request_id` | string | - | 精确匹配 |

**出参**：`dto.AdminUsageLog` 列表。

---

## B 组：用户端 `/api/v1/pool/*`

挂在 `authenticated` 组下（`jwtAuth` + `BackendModeUserGuard` + 限流 + 审计）。
**每个方法第一件事就是取 `subject.UserID` 并强制钉进查询条件**，前端传什么都覆盖不掉。

### B1 `GET /api/v1/pool/overview`

**入参**：无（`user_id` 从 token 取）。

**出参** `PoolUserOverview`：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "account_count": 1,
    "active_count": 1,
    "platform_counts": { "openai": 1 },
    "today": { "requests": 2, "tokens": 6200, "cost": 0.0185, "standard_cost": 0.0185, "user_cost": 0.0185 },
    "window_7d": { "requests": 2, "tokens": 6200, "cost": 0.0185, "standard_cost": 0.0185, "user_cost": 0.0185 },
    "generated_at": "2026-11-04T14:54:31+08:00"
  }
}
```

与 `PoolOverview` 相比**没有**：`total_accounts`、`error_accounts`、`disabled_accounts`、
`schedulable_count`、`total_in_use`。因为这些都是全站指标，用户看到就是泄露。

用户端的 `cost` 直接等于 `Σ user_cost`（实付），不看账号倍率。

### B2 `GET /api/v1/pool/accounts`

**入参**：`page` / `page_size` / `platform` / `type` / `status` / `search`
（`group_id` 被忽略 —— 分组是调度内部概念）。

**出参** `PoolUserAccountList`，每项 `PoolUserAccount`：

```json
{
  "id": 1,
  "name": "POOL-TEST-OAI-A",
  "platform": "openai",
  "type": "oauth",
  "status": "active",
  "schedulable": true,
  "last_used_at": "2026-11-04T14:41:20+08:00",
  "today": { "requests": 2, "tokens": 6200, "cost": 0.0185, "standard_cost": 0.0185, "user_cost": 0.0185 }
}
```

**就这么 8 个字段**。没有 `notes`、`error_message`、`rate_multiplier`、`priority`、
`concurrency`、`load_factor`、`in_use`、`groups`、`parent_account_id`、所有 `rate_limit_*`、
所有 `session_window_*`、`overload_until`、`temp_unschedulable_*`。

排序/过滤在**内存里做**：先按 7 天窗口取出该用户用过的账号 ID 集合，
再 `GetAccountsByIDs` 补全，然后在切片上做 platform/type/status/search 过滤与分页。
账号数量级是几十到几百，内存过滤完全够用，且避免了给 `accounts` 表加针对"用户维度"的查询。

### B3 `GET /api/v1/pool/accounts/:id/requests`

当前用户在指定账号上的请求流水。

**入参**：`page` / `page_size` / `start_time` / `end_time` / `model`

**出参**：`dto.UsageLog`（用户端 DTO）。关键：`filters.UserID = subject.UserID` **强制**，
即使用户传了 `account_id` 指向一个自己从未用过的账号，也只会得到空列表。

### B4 `GET /api/v1/pool/usage`

当前用户自己的号池使用记录。

**入参**：

| 参数 | 类型 | 默认 | 越界行为 |
|---|---|---|---|
| `page` / `page_size` | int | 1 / 20 | ≤0 修正，>200 截断 |
| `start_time` / `end_time` | RFC3339 | now-24h / now | 格式错 → 400 |
| `account_id` | int64 | - | ≤0 或非数字 → `400 Invalid account_id` |
| `api_key_id` | int64 | - | 同上 → `400 Invalid api_key_id` |
| `user_id` | int64 | - | **≠ 自己 → `403`** |
| `model` | string | - | |
| `request_id` | string | - | |

`user_id` 的处理是刻意的：

```go
if raw := strings.TrimSpace(c.Query("user_id")); raw != "" {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		response.BadRequest(c, "Invalid user_id")
		return
	}
	// 只允许查自己；越界明确拒绝，不静默忽略。
	if value != subject.UserID {
		response.Forbidden(c, "Not authorized to view other users' pool records")
		return
	}
}
```

**为什么 403 而不是静默忽略**：静默忽略会让攻击者的越权尝试看起来"正常返回了数据"，
他无法区分"我拿到了别人的"和"我拿到了自己的"；显式 403 让越权探测立刻暴露在审计日志里。

---

## 错误码汇总

| HTTP | code | message | 触发条件 |
|---|---|---|---|
| 400 | 400 | `Invalid account id` | `/accounts/:id/requests` 的 id 非正整数 |
| 400 | 400 | `Invalid account_id` | usage 的 `account_id` 非正整数 |
| 400 | 400 | `Invalid api_key_id` | 同上 |
| 400 | 400 | `Invalid user_id` | 同上 |
| 400 | 400 | `Invalid start_time` | RFC3339 解析失败 |
| 400 | 400 | `Invalid end_time` | RFC3339 解析失败 |
| 400 | 400 | `start_time must be before end_time` | 时间倒置 |
| 401 | 401 | `User not authenticated` | handler 内取不到 subject |
| 401 | 401 | `Invalid token` | JWT 无效/过期 |
| 403 | FORBIDDEN | `Admin access required` | 非 admin 访问 `/admin/*` |
| 403 | 403 | `Not authorized to view other users' pool records` | 用户端查别人的 `user_id` |
| 423 | ADMIN_COMPLIANCE_ACK_REQUIRED | `administrator compliance acknowledgement is required` | 管理员未接受合规承诺 |

## 时间参数的两个坑

1. **必须 RFC3339**。`2026-11-04T10:00:00+08:00` 或 `2026-11-04T02:00:00Z`。
   裸 `2026-11-04 10:00:00` 会被拒。
2. **`+08:00` 的 `+` 必须 URL 编码为 `%2B`**。不编码时服务端收到的 query 里
   `+` 已被解码成空格，`time.Parse` 直接失败 → `400 Invalid end_time`。
   用 `curl --data-urlencode` 或者干脆用 `Z` 结尾的 UTC 时间最省事。

## 管理员合规门槛（423）

`AdminComplianceGuard`（`backend/internal/server/middleware/admin_compliance.go`）
对所有 `/api/v1/admin/*` 生效，白名单只放行 `/api/v1/admin/compliance` 前缀。

首次访问任意 admin 接口 → `423`。解除方式：

```bash
curl -X POST "https://<host>/api/v1/admin/compliance/accept" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"phrase":"我已阅读、理解并同意 IKUN2api 部署与运营合规承诺","language":"zh"}'
# → {"code":0,"data":{"required":false,...}}
```

常量定义在 `backend/internal/service/admin_compliance.go`：
`AdminComplianceVersion = "v2026.06.10"`、`AdminComplianceAckPhraseZH`。
确认记录按 `admin_user_id` 落在 `admin_compliance_acknowledgements` 表——
**换一个管理员账号要重新接受**。