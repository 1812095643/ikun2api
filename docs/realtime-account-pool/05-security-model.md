# 安全模型：三层拦截与脱敏清单

## 1. 三层拦截

普通用户能不能看到号池数据、能看到多少，是三个独立的层各自把关的。
任何一层失效，都还有后面两层。

```
┌── 第 1 层：侧边栏菜单 ──────────────────────────────────────┐
│ AppSidebar.vue                                            │
│   管理端：整块 adminNavItems 包在 <template v-if="isAdmin">  │
│   用户端：buildSelfNavItems 里单独一条 /pool                │
│ 作用：不给入口。                                            │
└───────────────────────────────────────────────────────────┘
┌── 第 2 层：前端路由守卫 ────────────────────────────────────┐
│ router/index.ts:901                                        │
│   if (requiresAdmin && !authStore.isAdmin) next('/dashboard')│
│ 作用：手动输 URL 也进不去。                                  │
└───────────────────────────────────────────────────────────┘
┌── 第 3 层：服务端鉴权 ──────────────────────────────────────┐
│ /admin/*  → AdminAuthMiddleware → 非 admin 直接 403         │
│ /pool/*   → jwtAuth + 强制 subject.UserID                   │
│ 作用：真正的边界。前两层只是体验。                            │
└───────────────────────────────────────────────────────────┘
```

### 第 3 层的具体实现

管理端：`backend/internal/server/middleware/admin_auth.go`，`validateJWTForAdmin` 末尾

```go
if !user.IsAdmin() {
	AbortWithError(c, 403, "FORBIDDEN", "Admin access required")
	return
}
```

注册在 `routes/admin.go:27`，覆盖整个 `/admin` 组，所以 `/admin/pool/*` 自动受保护。

用户端：`routes/user.go` 的 `authenticated` 组已经挂了 `jwtAuth`，
handler 里再取一次 subject：

```go
subject, ok := middleware2.GetAuthSubjectFromContext(c)
if !ok {
	response.Unauthorized(c, "User not authenticated")
	return
}
```

`AuthSubject{UserID int64; Concurrency int}` 来自 JWT claims，
**不接受任何 query/body 覆盖**。

## 2. 用户端脱敏清单

删掉的字段及其理由：

| 字段 | 泄露什么 | 为什么删 |
|---|---|---|
| `rate_multiplier` | 该账号的计费倍率 | 商业敏感，且能被用来反推成本结构 |
| `priority` | 调度优先级 | 调度内部策略 |
| `concurrency` | 账号并发上限 | 同上 |
| `load_factor` | 负载权重 | 同上 |
| `in_use` | 当前有多少人正在用 | 能推断全站流量 |
| `groups` | 分组结构 | 组织内部划分 |
| `parent_account_id` | 账号父子关系 | 能反推账号池拓扑 |
| `notes` | 运营备注 | 可能含内部信息 |
| `error_message` | 上游原始报错 | 可能含凭据片段、上游 URL |
| `rate_limited_at` / `rate_limit_reset_at` | 限流时间 | 能推断上游配额窗口 |
| `overload_until` | 过载保护 | 内部保护机制 |
| `session_window_*` | 会话窗口 | 同上 |
| `temp_unschedulable_*` | 临时不可调度 | 同上 |
| `total_accounts` / `error_accounts` / `disabled_accounts` / `schedulable_count` | 全站账号总量与健康度 | 用户只需要知道自己用过几个 |
| `total_in_use` | 全站实时并发 | 同上 |
| `platform_counts`（全站） | 平台分布 | 用户端只给自己用过的账号做平台计数 |

### 用类型而不是 omitempty 来保证

`PoolUserAccount` 是一个独立结构体，字段表就只有 8 个：

```go
type PoolUserAccount struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	Platform     string     `json:"platform"`
	Type         string     `json:"type"`
	Status       string     `json:"status"`
	Schedulable  bool       `json:"schedulable"`
	LastUsedAt   *string    `json:"last_used_at,omitempty"`
	Today        PoolWindow `json:"today"`
}
```

**没有一个字段是"有值但被 omitempty 藏起来"**。如果哪天有人往 `PoolAccount`
加了新字段（比如 `quota_dimension`），用户端不会跟着泄露，因为它压根不读那个类型。

这是「白名单优于黑名单」在 API 层的应用：黑名单（列出要删的字段）会在字段增加时失效，
白名单（独立类型）不会。

### 平台快照也不给用户

`dto.UsageLog` 只加了 `account_name_snapshot`，**没加** `account_platform_snapshot`。
理由：账号名是"我给谁花了钱"的自然表达，平台名则能让人统计出池子里
各平台的数量和比例。

## 3. 越权处理策略：显式拒绝

用户端 `Usage` 里的 `user_id` 参数：

```go
if value != subject.UserID {
	response.Forbidden(c, "Not authorized to view other users' pool records")
	return
}
```

对比两种做法：

| 做法 | 攻击者视角 | 审计日志 | 结论 |
|---|---|---|---|
| 静默忽略（用 subject 覆盖） | 看到自己的数据，以为拿到了别人的 | 无痕迹 | 差 |
| 显式 403 | 立刻知道被拒 | 有痕迹 | **采用** |

同理 `account_id`：用户传一个自己没用过的账号 ID，**不报错**，返回空列表。
因为"这个账号我没用过"本身是合法信息，报错反而泄露了账号是否存在。
但 `user_id` 不同——那是在尝试换个人看，是明确的越权意图。

## 4. 实测证据（2026-11-04）

### 无 token

```
GET /api/v1/pool/overview       → 401
GET /api/v1/pool/accounts       → 401
GET /api/v1/pool/usage          → 401
GET /api/v1/admin/pool/overview → 401
GET /api/v1/admin/pool/accounts → 401
GET /api/v1/admin/pool/usage    → 401
```

### 普通用户 token 打管理端

```
GET /api/v1/admin/pool/overview → 403 {"code":"FORBIDDEN","message":"Admin access required"}
GET /api/v1/admin/pool/accounts → 403 同上
GET /api/v1/admin/pool/usage    → 403 同上
```

### 普通用户越界查别人

```
GET /api/v1/pool/usage?user_id=1
→ 403 {"code":403,"message":"Not authorized to view other users' pool records"}
```

### 普通用户实际拿到的字段（`/api/v1/pool/accounts`）

实测响应里每条只有：

```
id, last_used_at, name, platform, schedulable, status, today, type
```

对比管理端 20+ 字段，脱敏到位。

## 5. 一个已知的「看起来像漏洞」的现象

```bash
curl -i https://<host>/admin/pool
# HTTP/2 200，返回 index.html
```

这是 SPA 的 history 兜底：任何未匹配静态资源的路径都吐 `index.html`。
但页面加载后：

1. Vue Router 守卫看到 `requiresAdmin: true` 且 `isAdmin === false` → `next('/dashboard')`；
2. 即使强行留在页面上，所有数据请求都是 403。

**没有数据泄露**。要让这个返回 403 需要给 SPA 兜底路由加服务端判定，
但那会让「未登录用户直接访问深链接跳登录页」的体验变差，权衡后保持现状。

## 6. 只读保证

`AccountPoolService` 的注释是硬约束：

```go
// 本服务只做读侧聚合：
//   - 不新增写入路径
//   - 不改动调度、计费与鉴权
//   - 全部数据来自 accounts 表与 usage_logs 表
```

代码层面的体现：

- `registerPoolRoutes` 只有 `GET`，没有任何 `POST` / `PUT` / `PATCH` / `DELETE`；
- `AccountPoolService` 只调用 `ListAccounts` / `GetAccountsByIDs` / `GetAccountWindowStatsBatch` /
  `GetUserAccountWindowStatsBatch` / `GetPoolGlobalWindowStats` / `ListAccountIDsByUser` /
  `GetAccountConcurrencyBatch`——全是读方法；
- 前端页面没有任何表单提交，只有筛选与查询。

## 7. 数据可见性矩阵

| 数据 | 管理员 | 普通用户 |
|---|---|---|
| 账号名 | 全部 | 仅自己用过的 |
| 平台 / 类型 | 全部 | 仅自己用过的 |
| 账号状态 | 全部 | 仅自己用过的 |
| 可调度性 | 全部 | 仅自己用过的 |
| 最近使用时间 | 全部（全站） | 仅自己的记录范围 |
| 今日请求 / Tokens | 全站 | 仅自己 |
| 今日费用 | 账号侧成本（含倍率） | 自己实付 |
| 请求流水 | 全站任意账号 | 仅自己的 |
| 请求 ID / 模型 / 端点 / 耗时 | 全部 | 仅自己的 |
| 用户邮箱 | 有 | 无（只有自己的不在列表里显示） |
| API Key 名称 | 全部 | 仅自己的 |
| 上游模型 / 模型映射链 | 有 | **无** |
| 账号倍率 / 优先级 / 并发 / 分组 | 有 | **无** |
| 全站账号总数 / 错误数 / 实时并发 | 有 | **无** |