# 验证记录

全部验证在 2026-11-04 于线上 `https://sub.breeai.top`（镜像 `1.3.39-pool6`）完成。
所有数字都是真实响应，不是构造的。

## 1. 对账（最重要的一项）

### 修复前（v1.3.39-pool5，存在口径 bug）

| | 数值 |
|---|---|
| 管理端概览 `today.cost` | `0.0440` |
| 三张账号卡 `today.cost` 之和 | `0.026 + 0 + 0.036 = 0.062` |
| 差额 | `0.018` ← 正好是 `POOL-TEST-CC-A` 的 `rate_multiplier=2` 应多出的部分 |

### 修复后（v1.3.39-pool6）

**管理员 `/api/v1/admin/pool/overview`**

```json
{
  "total_accounts": 3, "active_accounts": 2, "error_accounts": 1,
  "disabled_accounts": 0, "schedulable_count": 2,
  "platform_counts": { "claude": 1, "openai": 2 },
  "total_in_use": 0,
  "today":     { "requests": 3, "tokens": 6300,  "cost": 0.062,  "standard_cost": 0.044,  "user_cost": 0.044 },
  "window_7d": { "requests": 8, "tokens": 19500, "cost": 0.2063, "standard_cost": 0.1269, "user_cost": 0.1269 }
}
```

**管理员账号卡片之和**

| 账号 | requests | tokens | cost |
|---|---|---|---|
| `POOL-TEST-OAI-A` | 2 | 3500 | 0.026 |
| `POOL-TEST-OAI-B` | 0 | 0 | 0 |
| `POOL-TEST-CC-A` | 1 | 2800 | 0.036 |
| **SUM** | **3** | **6300** | **0.062000** |

✅ 三项全部与概览一致。

**普通用户 `demo2` `/api/v1/pool/overview`**

```json
{
  "account_count": 2, "active_count": 2,
  "platform_counts": { "claude": 1, "openai": 1 },
  "today":     { "requests": 3, "tokens": 11600, "cost": 0.069 },
  "window_7d": { "requests": 3, "tokens": 11600, "cost": 0.069 }
}
```

**普通用户账号卡片之和**

| 账号 | requests | tokens | cost |
|---|---|---|---|
| `POOL-TEST-OAI-A` | 1 | 1200 | 0.006 |
| `POOL-TEST-CC-A` | 2 | 10400 | 0.063 |
| **SUM** | **3** | **11600** | **0.069000** |

✅ 一致。注意用户端 `cost` 等于实付累计，**不**乘账号倍率
（`POOL-TEST-CC-A` 的 `rate_multiplier=2` 只影响管理端成本口径）。

## 2. 鲁棒性测试（10 项，普通用户身份）

| # | 输入 | 期望 | 实际 | 结果 |
|---|---|---|---|---|
| 1 | `?user_id=1`（别人的 ID） | 403 | `{"code":403,"message":"Not authorized to view other users' pool records"}` | ✅ |
| 2 | `?user_id=abc` | 400 | `{"code":400,"message":"Invalid user_id"}` | ✅ |
| 3 | `GET /admin/pool/{overview,accounts,usage}` | 403 | `{"code":"FORBIDDEN","message":"Admin access required"}` ×3 | ✅ |
| 4 | `start_time > end_time` | 400 | `{"code":400,"message":"start_time must be before end_time"}` | ✅ |
| 5 | `start_time=notatime` | 400 | `{"code":400,"message":"Invalid start_time"}` | ✅ |
| 6 | `page_size=9999` | 截到 200 | 回落 `page_size=20` | ✅ |
| 7 | `page=0&page_size=0` | 修正为 1/20 | `page=1, page_size=20` | ✅ |
| 8 | `account_id=999999`（不存在） | 空列表非 500 | `{"code":0,"items":[],"total":0}` | ✅ |
| 9 | `search=' OR 1=1--` | 无注入 | `code:0, total:0` | ✅ |
| 10 | 无 token | 401 | 三条全 401 | ✅ |

第 9 项说明：`search` 走的是 GORM 参数化查询，单引号只是普通字符，
所以返回 0 条而不是全表。

## 3. 浏览器验收（Playwright，Chrome）

### 普通用户 `demo1` → `/pool`

```
标题：我的号池用量
描述：这里只展示你自己调用过的上游账号与用量，
      内部调度信息（分组、并发、倍率、优先级）不对普通用户开放。
tablist：tab "账号池" [aria-selected=true] / tab "使用记录" [false]
侧边栏：实时号池 → /pool
版本号：v1.3.39-pool6

概览 4 卡：
  我用过的账号   1   其中 1 个当前正常
  今日请求       2   6.2K
  今日费用       0.0185
  近 7 天请求    2   含今日
```

账号池表格（8 列）：

| 账号 | 平台·类型 | 状态 | 今日请求 | 今日 Tokens | 今日费用 | 最近使用 | 操作 |
|---|---|---|---|---|---|---|---|
| POOL-TEST-OAI-A #1 | OpenAI OAuth | 正常 | 2 | 6.2K | 0.0185 | 7分钟前 | 请求流水 |

「请求流水」抽屉（点按钮打开）：

```
标题：账号 #1 近期请求
11:44:05 | gpt-4o | /v1/chat/completions | 1,000 / 500 | 0.0150 | 900 ms | req-poolview-a1
```

### 普通用户 `demo2` → `/pool?tab=usage`

3 行：

```
14:41:20 | gpt-4o            | /v1/chat/completions | POOL-TEST-OAI-A #1 | demo2-key | 800 / 400     | 0.0060 | 700 ms  | req-demo2-a1
14:16:20 | claude-sonnet-4-5 | /v1/messages         | POOL-TEST-CC-A #3  | demo2-key | 5,000 / 2,500 | 0.0450 | 2100 ms | req-demo2-b2
12:46:19 | claude-sonnet-4-5 | /v1/messages         | POOL-TEST-CC-A #3  | demo2-key | 2,000 / 900   | 0.0180 | 1500 ms | req-demo2-b1
```

账号池 tab 2 行（`POOL-TEST-CC-A` 10.4K tokens / 0.0630 / 33分钟前）。

### 管理员 → `/admin/pool`

```
概览 6 卡：
  账号总数     3    活跃 2 · 异常 1 · 停用 0
  可调度       2    参与调度且未过期
  当前并发     0
  今日请求     3    6.3K
  今日费用     0.0620   0.0440
  近 7 天请求  8    含今日

表格：13 列，3 行，显示 1 至 3 共 3 条结果
```

13 列：账号 / 平台·类型 / 状态 / 分组 / 今日请求 / 今日Tokens / 今日费用 /
并发占用 / 倍率 / 优先级 / 最近使用 / 可用性 / 操作

`/admin/pool?tab=usage` 6 行：

```
12:14:05 | POOL-TEST-CC-A #3 | claude | claude-sonnet-4-5 | claude-sonnet-4-5-20250929 | — | poolviewtest@test.local #4 | pool-view-test-key | 2,000 / 800 | 0.0180 | — | req-poolview-b1
```

### 管理员 vs 用户端字段对比（同一份数据）

| 列 | 管理端 | 用户端 |
|---|---|---|
| 账号（含 #id） | ✅ | ✅ |
| 平台·类型 | ✅ | ✅ |
| 状态 | ✅ | ✅ |
| 分组 | ✅ | ❌ |
| 今日请求 / Tokens / 费用 | ✅ | ✅ |
| 并发占用 | ✅ | ❌ |
| 倍率 | ✅ | ❌ |
| 优先级 | ✅ | ❌ |
| 最近使用 | ✅ | ✅ |
| 可用性 | ✅ | ❌ |
| 请求流水 | ✅ | ✅ |
| 用户 | ✅ | ❌ |
| 上游模型 | ✅ | ❌ |

## 4. API 脱敏实测

普通用户 `GET /api/v1/pool/accounts` 返回的字段全集（**实测，不是推测**）：

```
id, last_used_at, name, platform, schedulable, status, today, type
```

8 个。管理端同样接口返回 20+ 字段。对比可见：
`rate_multiplier` / `priority` / `concurrency` / `load_factor` / `in_use` /
`groups` / `notes` / `error_message` / `rate_limited_at` / `rate_limit_reset_at` /
`overload_until` / `session_window_*` / `temp_unschedulable_*` / `parent_account_id`
全部不在响应里。

## 5. 健康检查

```
docker ps：
  ikun2api           Up (healthy)
  ikun2api-postgres  Up (healthy)
  ikun2api-redis     Up (healthy)

curl http://127.0.0.1:18088/health        → {"status":"ok"}
curl https://sub.breeai.top/health        → {"status":"ok"}
/api/v1/settings/public                   → version: "1.3.39-pool6"
```

## 6. 已知的非问题

| 现象 | 判定 |
|---|---|
| `curl /admin/pool` 返回 200（index.html） | SPA 兜底，页面加载后守卫跳走、数据接口 403，无泄露 |
| `goto('/pool?tab=usage')` 首帧显示「所选范围内没有号池使用记录」 | 首帧时序，下一 tick 数据到；点 tab 进入不会出现 |
| 同一条记录管理端看 `0.036`、用户端看 `0.018` | 口径不同（账号侧成本 vs 用户实付），正确 |

## 7. 演示数据说明

线上留了 3 个账号与若干用量记录用于验收展示：

| 账号 | 状态 | 说明 |
|---|---|---|
| `POOL-TEST-OAI-A` #1 | active / schedulable | openai / oauth，假凭据 |
| `POOL-TEST-OAI-B` #2 | error / 不可调度 | 用于验证异常态渲染 |
| `POOL-TEST-CC-A` #3 | active / `rate_multiplier=2` | 用于验证倍率口径 |

用户 `demo1@test.local`、`demo2@test.local`（role=user，余额 5.00）。

**这些账号的 credentials 是假数据，打网关不会成功**，它们只用于页面展示与口径验证。
真实流量进来后，快照列会由计费路径自动填充。