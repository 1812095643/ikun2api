# 实时号池（Realtime Account Pool）

面向运营的**只读**账号池观测视图。一个页面、两个标签（账号池 / 使用记录），
同一份代码按访问者身份自动切换「全量管理视图」与「本人脱敏视图」。

- 管理端入口：`/admin/pool`
- 用户端入口：`/pool`
- 线上版本：`1.3.39-pool6`
- 首版落地日期：2026-11-04

## 这个功能解决什么问题

原来的「账号管理」页是**写操作中心**：新建、编辑、测试、禁用、改倍率。
运营真正高频的需求不是改，而是**看**：

1. 池子里现在有几个账号、几个能用、几个在报错、几个被限流；
2. 每个账号今天跑了多少请求、多少 token、平台侧花了多少钱；
3. 某个账号最近到底被谁在用、跑的是哪个模型、走的哪个端点；
4. 用户侧会问「我的钱花在哪个账号上了」，而账号名可能已经被改过或删掉。

第 4 点决定了本功能最关键的一条设计：**用量记录必须自带快照**，
不能靠 join `accounts` 表实时取名字。详见 `02-data-model.md`。

## 与既有页面的边界

| | 账号管理 `/admin/accounts` | 实时号池 `/admin/pool` |
|---|---|---|
| 性质 | 读写 | **只读**（无任何写入路径） |
| 关注点 | 配置（凭据、代理、分组、倍率） | 运行态（状态、用量、流水） |
| 是否改动调度 | 是 | **否**，不碰调度/计费/鉴权 |

代码里 `AccountPoolService` 的注释明确写了这条约束：

```go
// 本服务只做读侧聚合：
//   - 不新增写入路径
//   - 不改动调度、计费与鉴权
//   - 全部数据来自 accounts 表与 usage_logs 表
```

## 文档索引

| 文档 | 内容 |
|---|---|
| [01-architecture.md](01-architecture.md) | 分层结构、数据流、为什么这样切 |
| [02-data-model.md](02-data-model.md) | 新增列、迁移、**费用口径**（易错点） |
| [03-backend-api.md](03-backend-api.md) | 8 个接口的完整契约、参数、错误码 |
| [04-frontend.md](04-frontend.md) | 单页双标签、按身份拼装列、状态管理 |
| [05-security-model.md](05-security-model.md) | 三层拦截、脱敏清单、越权处理策略 |
| [06-build-deploy.md](06-build-deploy.md) | 无 buildx 环境构建、部署切换、回滚 |
| [07-verification.md](07-verification.md) | 对账记录、10 项鲁棒性测试、浏览器验收 |
| [08-changelog.md](08-changelog.md) | 版本演进、已知限制、后续 TODO |

## 30 秒上手

```bash
# 管理端（需要 admin 角色）
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  "https://<host>/api/v1/admin/pool/overview"

# 用户端（任意登录用户，服务端强制只返回本人数据）
curl -H "Authorization: Bearer $USER_TOKEN" \
  "https://<host>/api/v1/pool/overview"
```

浏览器：

- 管理员登录 → 侧边栏「实时号池」→ `/admin/pool`
- 普通用户登录 → 侧边栏「实时号池」→ `/pool`（标题变成「我的号池用量」）

## 三条不能破的约束

1. **只读**。这个功能不提供任何写接口，页面里也没有编辑入口。要改账号请去账号管理。
2. **用户端脱敏在服务端做**。不是前端 `v-if` 藏起来，是 DTO 里根本没有那些字段。
3. **概览数字必须等于卡片之和**。任何口径分歧都算 bug，见 `02-data-model.md` 的「费用口径」。