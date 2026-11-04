# 架构与数据流

## 分层

```
┌─────────────────────────────────────────────────────────────┐
│ 前端  PoolView.vue（单文件，两个标签）                        │
│   scope = isAdmin ? 'admin' : 'user'                        │
└───────────────┬─────────────────────────┬───────────────────┘
                │ /admin/pool/*           │ /pool/*
                ▼                         ▼
┌───────────────────────────┐  ┌──────────────────────────────┐
│ handler/admin.PoolHandler │  │ handler.PoolHandler           │
│  · 无身份钉住             │  │  · 每方法强制 subject.UserID  │
│  · 返回 AdminUsageLog     │  │  · 返回 UsageLog（脱敏 DTO）  │
└───────────────┬───────────┘  └──────────────┬───────────────┘
                └───────────┬─────────────────┘
                            ▼
              ┌─────────────────────────────────┐
              │ service.AccountPoolService      │
              │  Overview / ListAccounts        │  ← 管理端
              │  OverviewForUser                │  ← 用户端
              │  ListAccountsForUser            │
              └───────┬─────────────┬───────────┘
                      │             │
        ┌─────────────▼──┐   ┌──────▼────────────────────┐
        │ AdminService   │   │ UsageLogRepository        │
        │ GetAccountsByID│   │  GetAccountWindowStats…   │
        │ ListAccounts   │   │  GetUserAccountWindowStats│
        └────────────────┘   │  GetPoolGlobalWindowStats │
                             │  ListAccountIDsByUser     │
                             └───────────────────────────┘
```

## 为什么管理端和用户端共用一个 service

两条需求几乎同构：都是「账号列表 + 今日用量」，差别只在**数据范围**和**字段裁剪**。

如果拆成两个 service，账号过滤、状态判定（`poolExpired`）、时间窗口计算、
并发占用聚合这些逻辑要写两遍，且容易在某次修复里只改一边（这个 bug 在
`02-data-model.md` 里真实发生过一次，就是费用口径）。

所以采用：**同一 service，两个入口方法**。

- `Overview(ctx)` → `PoolOverview`（全站）
- `OverviewForUser(ctx, userID)` → `PoolUserOverview`（单人）

选择用**不同的返回类型**而不是同一个类型加 omitempty，因为脱敏要求是
"字段不存在"，不是"字段为空"。`PoolUserAccount` 里根本没有
`RateMultiplier`、`Priority`、`Groups` 这些字段，编译期就保证了不会漏。

## 可选能力的取法（本项目既定模式）

`UsageLogRepository` 是个约 50 个方法的大接口，被大量测试桩实现。
往里加方法会逼所有桩跟着改。所以号池需要的新查询走**类型断言**：

```go
// account_pool_service.go:399
type poolGlobalStatsReader interface {
	GetPoolGlobalWindowStats(ctx context.Context, startTime time.Time) (*usagestats.AccountStats, error)
}

// account_pool_service.go:574
func (s *AccountPoolService) poolGlobalWindowStats(ctx context.Context, startTime time.Time) *usagestats.AccountStats {
	reader, ok := s.usageLogRepo.(poolGlobalStatsReader)
	if !ok {
		return nil // 故意不兜底，见下
	}
	...
}
```

同样的模式在用户端：

```go
// account_pool_service.go:403
type userPoolStatsReader interface {
	ListAccountIDsByUser(ctx context.Context, userID int64, startTime time.Time) ([]int64, error)
	GetUserAccountWindowStatsBatch(ctx context.Context, userID int64, accountIDs []int64, startTime time.Time) (map[int64]*usagestats.AccountStats, error)
}
```

**为什么不 fallback 到 `GetGlobalStats`**：那个方法返回的 `TotalAccountCost`
是裸 `SUM(account_stats_cost)`，不含账号倍率。宁可显示 0，也不要显示一个
跟卡片对不上的数字（用户会以为系统算错钱）。

## 数据流：一次 `/admin/pool` 打开

1. `onMounted` → `refreshAll()` → 并发两个请求
   - `GET /api/v1/admin/pool/overview`
   - `GET /api/v1/admin/pool/accounts?page=1&page_size=20`
2. `Overview`：
   - `dayStart = timezone.Today()`，`weekStart = dayStart.AddDate(0,0,-6)`
   - `GetPoolGlobalWindowStats(dayStart)` → `PoolWindow{...}`
   - `GetPoolGlobalWindowStats(weekStart)` → 同上
   - 账号维度：`ListAccounts` 内部再查一次最近 7 天（`poolOverviewPages`）
3. `ListAccounts`：
   - `adminService.ListAccounts(...)` 拿账号基础信息
   - `fillTodayStats` → `GetAccountWindowStatsBatch(ids, dayStart)`
   - `fillConcurrency` → `GetAccountConcurrencyBatch(ids)`
4. 前端把 `overview.today` 渲染成概览卡，把 `items[].today` 渲染成表格列。

## 数据流：普通用户打开 `/pool`

1. 同一个组件，`scope = 'user'` → 请求 `/api/v1/pool/*`
2. `OverviewForUser(userID)`：
   - `ListAccountIDsByUser(userID, weekStart)` → 该用户 7 天内用过哪些账号
   - `GetAccountsByIDs(ids)` → 补平台/状态
   - `GetUserAccountWindowStatsBatch(userID, ids, dayStart)` → 今日
   - `GetUserAccountWindowStatsBatch(userID, ids, weekStart)` → 近 7 天
3. 返回 `PoolUserOverview`（没有 `error_accounts` / `disabled_accounts` / `total_in_use`）

**为什么账号集合按 7 天取而不是按今天**：用户今天没调用，不代表他不关心
自己昨天用过的账号。如果按今日取，早上打开页面会看到"空号池"，不像话。

## 路由注册位置

| 文件 | 位置 | 内容 |
|---|---|---|
| `backend/internal/server/routes/admin.go` | `:51` | `registerPoolRoutes(admin, h)` |
| `backend/internal/server/routes/admin.go` | `:750-758` | 4 条 admin 路由 |
| `backend/internal/server/routes/user.go` | `:98-107` | 4 条 user 路由 |

admin 组整体挂了 `adminAuth`（`admin.go:27`），所以 `/admin/pool/*` 天然要求 admin 角色。
user 组的 `authenticated` 已经挂了 `jwtAuth` + `BackendModeUserGuard`，所以 `/pool/*` 天然要求登录。

## 依赖注入的手工同步

本项目没有 `internal/handler/wire_gen.go`，只有 `backend/cmd/server/wire_gen.go`。
改了 `ProvideHandlers` 签名后必须手工同步生成文件，否则编译期就报参数数量不匹配：

```go
// backend/cmd/server/wire_gen.go:303-305
accountPoolService := service.NewAccountPoolService(adminService, accountUsageService, concurrencyService, usageLogRepository)
poolHandler := admin.NewPoolHandler(accountPoolService, usageService)
userPoolHandler := handler.NewPoolHandler(accountPoolService, usageService)
```

## 目录速查

```
backend/
  internal/
    handler/
      admin/pool_handler.go        # 管理端 4 方法
      pool_handler.go              # 用户端 4 方法（新）
      handler.go                   # Handlers 结构加 Pool 字段
      wire.go                      # ProviderSet + ProvideHandlers 签名
      dto/types.go                 # UsageLog 加 AccountNameSnapshot
      dto/mappers.go               # 两个 mapper 赋值
    service/
      account_pool_service.go      # 核心聚合（新，610 行）
      usage_log_account_snapshot.go# 快照辅助函数（新）
      usage_log.go                 # UsageLog 加两个快照字段
      gateway_usage_billing.go     # 写入点 1
      openai_gateway_usage.go      # 写入点 2
      wire.go                      # ProviderSet 加 NewAccountPoolService
    repository/
      usage_log_repo_stats.go      # 3 个新统计方法
      usage_log_repo_insert.go     # 插入列 +2（含批量 SQL）
      usage_log_repo_query.go      # SELECT 列 +2 + scan
    server/routes/
      admin.go                     # registerPoolRoutes
      user.go                      # /pool 组
  migrations/
    263_usage_log_account_snapshot.sql
frontend/src/
  views/admin/PoolView.vue         # 单页双标签（新，1179 行）
  api/pool.ts                      # scope 参数（新，177 行）
  i18n/locales/{zh,en}/admin/pool.ts
  router/index.ts                  # /admin/pool + /pool
  components/layout/AppSidebar.vue # 两个菜单项
  config/menuPages.ts              # 菜单可见性登记
  types/index.ts                   # AdminUsageLog 加两个快照字段
```