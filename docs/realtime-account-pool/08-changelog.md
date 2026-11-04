# 版本演进与后续计划

## 版本时间线

本功能在一天内迭代了 7 个镜像，每个都是真实部署 + 真实验证后才发现的问题。
这里如实记录，因为**踩坑过程比最终代码更有参考价值**。

| 镜像 | 主要变化 | 结果 |
|---|---|---|
| `1.3.39-pool` | 后端 4 个 admin 接口 + 两个独立页面 | 首个可用版本 |
| `1.3.39-pool2` | 修 `GetGlobalStats` 口径，`overview.cost` 从恒为 0 变成真实值 | 通过 |
| `1.3.39-pool3` | 单页双标签重构（PoolView.vue）+ 用户端 4 个接口 | 构建成功 |
| `1.3.39-pool4` | 用户端改返回独立脱敏类型 `PoolUserAccount`；`poolSumUserWindows` 改用 `UserCost` | 通过 |
| `1.3.39-pool5` | 用户端 DTO 补 `account_name_snapshot`（修「账号已删除」）；前端补 `endpoint`/`duration` 取值 | 通过 |
| `1.3.39-pool6` | 新增 `GetPoolGlobalWindowStats`（修概览与卡片口径不一致） | **当前线上** |
| `1.3.39-pool7` | 本文档入库 + `dev-gw` 分支 | — |

## 每个坑的详细说明

### 坑 1：`overview.cost` 恒为 0

`GetGlobalStats` 返回的 `UsageStats` 里没有 `account_stats_cost` 聚合，
`poolWindowFromStats` 映射到 `PoolWindow.Cost` 时取了个不存在的字段 → 0。

修法：新增 `GetPoolGlobalWindowStats` 直接返回 `usagestats.AccountStats`，
字段一一对应，不经中间类型转换。

### 坑 2：概览与卡片对不上账

见 `02-data-model.md` 的「费用口径」章节。核心是**两条 SQL 必须逐字对齐**，
而不是"大概都是 SUM"。

**教训**：任何"总览数字"和"明细之和"同时出现在一个页面上的功能，
上线前必须跑一遍 `总览 == Σ明细` 的断言。这不是可选的验收项。

### 坑 3：用户端把账号显示成「账号已删除」

前端 `accountLabel` 的降级链依赖 `account_name_snapshot`，但用户端 DTO 没这个字段。
所以正确做法不是改前端降级链，而是**给用户端 DTO 补这一个字段**
（只补 name，不补 platform）。

### 坑 4：`endpoint` / `duration` 列永远为空

`DataTable` 用 `row[column.key]` 取值，列 key 与 DTO 字段名不一致时静默为空，
没有任何报错。凡是 DTO 字段名和列 key 不同的列，都必须写成作用域插槽手动取值。

### 坑 5：构建机没有 buildx

见 `06-build-deploy.md`。这个坑会反复出现，因为生产机不会为了构建去升级 docker。

### 坑 6：Node 堆溢出

`FATAL ERROR: Ineffective mark-compacts near heap limit` → exit 134。
3.7G 内存的机器编 1392 个模块必须 `--max-old-space-size=3072`。

### 坑 7：`--platform=${BUILDPLATFORM}` 与 `--mount=type=cache`

legacy builder 不认识这两个语法，报错信息都很难懂
（`"" is an invalid OS component` / `the --mount option requires BuildKit`）。

### 坑 8：盘写满导致全站服务挂掉

构建残留的悬空镜像累积到 12.5G，59G 的盘用到 53G，postgres 写不进去 → 全部服务挂。
清理后回到 36G/63%。

**教训**：每次构建完立刻 `docker image prune -f`，不要攒。

### 坑 9：psql heredoc 事务不生效

```bash
psql -v ON_ERROR_STOP=1 <<'SQL'
BEGIN;
DELETE ...;
COMMIT;
SQL
```
在 ssh 管线下**无报错但不提交**。改逐条执行。

### 坑 10：`+08:00` 的 `+` 未编码

`start_time=2026-11-04T10:00:00+08:00` 传到服务端时 `+` 已变成空格 →
`400 Invalid end_time`。用 `--data-urlencode` 或 `Z` 结尾 UTC。

## 已知限制

### 1. 只有「今日」和「近 7 天」两个窗口

PRD 最初设想过 5 小时 / 1 小时的滑动窗口（对齐 Anthropic 的 5h 限流窗口），
实际实现只做了 `today` 与 `window_7d`。原因：

- 5h 窗口需要按账号维护滚动计数的额外状态，而 `usage_logs` 上没有对应的复合索引；
- 现有 `api_keys` 表的 `rate_limit_5h` / `usage_5h` 已经覆盖了限流侧需求，
  号池视图再算一遍意义不大。

如需补齐，SQL 层面只需把 `startTime` 换成 `now - 5h`，服务层已参数化。

### 2. 用户端账号列表在内存里过滤与分页

`ListAccountsForUser` 先取全部账号再在切片上过滤/分页。
账号数量级（几十到几百）下完全够用；如果将来单实例上万账号，
需要改成 SQL 侧分页。

### 3. 用户端不显示平台快照

只给了 `account_name_snapshot`。如果运营希望用户也能看到"我这次走的是哪家上游"，
需要评估是否泄露池子结构。

### 4. 老的用量记录没有快照

迁移新增的两列对历史行为 NULL，这些行在用户端会退化到「账号已删除」文案。
无法回填，因为当时的账号可能已经改名或删除，回填等于伪造历史。

### 5. 没有导出功能

运营目前靠截图/复制。如需 CSV 导出，可以在前端用已加载的数据生成，不必新增接口。

## 后续候选（按价值排序）

| 优先级 | 事项 | 说明 |
|---|---|---|
| P0 | 5h 滑动窗口 | 对齐上游限流周期，运营最关心 |
| P0 | 账号健康趋势图 | 当前只有瞬时状态，看不出"这个账号最近老出错" |
| P1 | 用量导出 CSV | 前端生成即可 |
| P1 | 按分组聚合视图 | 现在只能看单个账号 |
| P2 | 用户端显示平台快照 | 需评估 |
| P2 | 告警联动 | 账号 error 时通知 |

## 与 `main` 分支的关系

本功能在 `dev-gw` 分支开发。开发期间 `origin/main` 前进了一个提交
`693c4e2`（`fix(claude): 适配 2.1.318 隐私过滤和 provenance 协议联动`），
改动 16 个文件，全部集中在 claude_privacy / claude_telemetry / gateway_request，
与号池改动**零文件重叠**，合并不会有冲突。

合并时建议顺序：

```bash
git checkout main
git merge dev-gw          # 预期无冲突
# 或
git checkout dev-gw
git rebase main           # 逐个 commit 应用，历史更干净
```

## 代码量与构成

| 类别 | 文件数 | 行数 |
|---|---|---|
| 后端 service | 2 | 646 |
| 后端 handler | 2 | 446 |
| 后端 repository / 迁移 | 4 | ~80 改动 |
| 前端视图 | 1 | 1179 |
| 前端 api / i18n / 配置 | 5 | ~320 |
| 文档 | 9 | 约 1800 |

新增未跟踪文件 12 个共 3871 行，已跟踪文件改动 23 个 +291/−10。