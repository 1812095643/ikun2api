# 前端：单页双标签 + 按身份自适应

## 1. 为什么合并成一页

改造前是侧边栏两个子项（`expandOnly: true` + 两个 children），点开各自是个独立页面。
问题：

- 运营看账号时会想问"这个账号刚才谁在用"，得切页面、重新填筛选条件；
- 两个页面的筛选条件、时间范围互不相通；
- 用户侧要展示的其实是同一份东西的脱敏版，做两套页面等于维护两份逻辑。

改造后：一个路由、一个组件、页内两个 tab，**URL 里带 `?tab=`**，可分享可刷新。

## 2. 文件结构

```
frontend/src/views/admin/PoolView.vue    1179 行，单文件承载两个标签
frontend/src/api/pool.ts                 177 行，scope 参数化
frontend/src/i18n/locales/zh/admin/pool.ts
frontend/src/i18n/locales/en/admin/pool.ts
```

组件模板骨架：

```vue
<AppLayout>
  <TablePageLayout>
    <template #filters>
      <!-- tab 卡片 -->
      <div role="tablist">
        <button v-for="tab in tabItems" :key="tab.key"
                role="tab" :aria-selected="activeTab === tab.key"
                @click="switchTab(tab.key)">
          {{ tab.label }}
        </button>
      </div>

      <!-- 账号池：概览卡 + 筛选行 -->
      <template v-if="activeTab === 'accounts'">
        <div v-for="card in overviewCards" :key="card.key">...</div>
        <input v-model="filters.search" @keyup.enter="search" />
        <Select v-model="filters.platform" :options="platformOptions" />
        ...
      </template>

      <!-- 使用记录：日期范围 + 五个输入 -->
      <template v-else>
        <DateRangePicker v-model:start-date="usageStart" v-model:end-date="usageEnd" />
        ...
      </template>
    </template>

    <template #table>
      <DataTable v-if="activeTab === 'accounts'" :columns="accountColumns" :rows="accountRows">
        <!-- 13 列具名插槽 -->
      </DataTable>
      <DataTable v-else :columns="usageColumns" :rows="usageRows">
        <!-- 12 列具名插槽 -->
      </DataTable>
    </template>

    <template #pagination>
      <Pagination v-if="activeTab === 'accounts'" ... />
      <Pagination v-else ... />
    </template>
  </TablePageLayout>

  <BaseDialog v-model:open="requestsVisible" :title="`账号 #${activeAccount?.id} 近期请求`">
    <!-- 7 列表 + 内嵌 Pagination -->
  </BaseDialog>
</AppLayout>
```

## 3. 身份判定与 scope

```ts
const authStore = useAuthStore()
const isAdmin = computed(() => authStore.isAdmin)
const scope = computed<PoolScope>(() => (isAdmin.value ? 'admin' : 'user'))
```

`authStore.isAdmin` 定义在 `frontend/src/stores/auth.ts:95`：

```ts
const isAdmin = computed(() => user.value?.role === 'admin')
```

`scope` 直接决定 API base：

```ts
// api/pool.ts
export type PoolScope = 'admin' | 'user'
const adminBase = '/admin/pool'
const userBase = '/pool'
function poolBase(scope: PoolScope): string {
  return scope === 'user' ? userBase : adminBase
}
```

四个函数都多了 `scope` 参数和 `signal` 参数：

```ts
export function getPoolOverview(scope: PoolScope = 'admin', options?: { signal?: AbortSignal })
export function listPoolAccounts(params: PoolAccountQuery, scope: PoolScope = 'admin', options?: { signal?: AbortSignal })
export function listPoolAccountRequests(accountId: number, params: PoolUsageQuery, scope: PoolScope = 'admin', options?: { signal?: AbortSignal })
export function listPoolUsage(params: PoolUsageQuery, scope: PoolScope = 'admin', options?: { signal?: AbortSignal })
```

## 4. 列定义按身份拼装

账号表的列是 `computed`，管理端 13 列、用户端 8 列：

```ts
const accountColumns = computed<Column[]>(() => {
  const base = [
    { key: 'account',        label: t('admin.pool.columns.account') },
    { key: 'platform_type',  label: t('admin.pool.columns.platformType') },
    { key: 'status',         label: t('admin.pool.columns.status') },
  ]
  if (isAdmin.value) {
    base.push({ key: 'groups',       label: t('admin.pool.columns.groups') })
  }
  base.push(
    { key: 'today_requests', label: t('admin.pool.columns.todayRequests') },
    { key: 'today_tokens',   label: t('admin.pool.columns.todayTokens') },
    { key: 'today_cost',     label: t('admin.pool.columns.todayCost') },
  )
  if (isAdmin.value) {
    base.push(
      { key: 'concurrency',     label: t('admin.pool.columns.concurrency') },
      { key: 'rate_multiplier', label: t('admin.pool.columns.rateMultiplier') },
      { key: 'priority',        label: t('admin.pool.columns.priority') },
    )
  }
  base.push(
    { key: 'last_used_at', label: t('admin.pool.columns.lastUsed') },
  )
  if (isAdmin.value) {
    base.push({ key: 'availability', label: t('admin.pool.columns.availability') })
  }
  base.push({ key: 'actions', label: t('admin.pool.columns.actions') })
  return base
})
```

| 列 | 管理端 | 用户端 |
|---|---|---|
| 账号 | ✅ | ✅ |
| 平台·类型 | ✅ | ✅ |
| 状态 | ✅ | ✅ |
| 分组 | ✅ | ❌ |
| 今日请求 / Tokens / 费用 | ✅ | ✅ |
| 并发占用 | ✅ | ❌ |
| 倍率 | ✅ | ❌ |
| 优先级 | ✅ | ❌ |
| 最近使用 | ✅ | ✅ |
| 可用性 | ✅ | ❌ |
| 操作（请求流水） | ✅ | ✅ |

使用记录表的列同理：用户端去掉「用户」列和「upstream 模型」列，管理员才有 `user_id` 筛选框。

## 5. 费用取值必须分身份

两条 helper：

```ts
function windowCost(w: PoolWindow): number {
  return isAdmin.value ? (w.cost ?? 0) : (w.user_cost ?? 0)
}

function rowCost(row: AdminUsageLog & UsageLog): number {
  return isAdmin.value
    ? (row.account_stats_cost ?? row.total_cost)
    : (row.actual_cost ?? row.total_cost)
}
```

管理端看**账号侧成本**（含倍率），用户端看**自己实付**。同一条记录在两个视角下
数字本来就不一样，这不是 bug。

## 6. 概览卡

```ts
const overviewCards = computed(() => {
  if (isAdmin.value) {
    return [
      { key: 'accounts',        value: overview.total_accounts,    hint: `${overview.active_accounts} / ${overview.error_accounts} / ${overview.disabled_accounts}` },
      { key: 'schedulable',     value: overview.schedulable_count, hint: t('admin.pool.overview.schedulableHint') },
      { key: 'in_use',          value: overview.total_in_use,      hint: t('admin.pool.overview.inUseHint') },
      { key: 'today_requests',  value: overview.today.requests,    sub: formatTokensK(overview.today.tokens) },
      { key: 'today_cost',      value: formatCostFixed(overview.today.cost), sub: formatCostFixed(overview.today.standard_cost) },
      { key: 'week_requests',   value: overview.window_7d.requests, hint: t('admin.pool.overview.weekRequestsHint') },
    ]
  }
  return [
    { key: 'accounts',       value: overview.account_count,       hint: t('admin.pool.user.overview.accountsHint', { active: overview.active_count }) },
    { key: 'today_requests', value: overview.today.requests,      sub: formatTokensK(overview.today.tokens) },
    { key: 'today_cost',     value: formatCostFixed(overview.today.cost) },
    { key: 'week_requests',   value: overview.window_7d.requests, hint: t('admin.pool.overview.weekRequestsHint') },
  ]
})
```

管理端 6 张卡、用户端 4 张。

## 7. tab 切换与 URL 同步

```ts
type PoolTab = 'accounts' | 'usage'

const initialTab: PoolTab = route.query.tab === 'usage' ? 'usage' : 'accounts'
const activeTab = ref<PoolTab>(initialTab)
const usageLoaded = ref(false)

function switchTab(tab: PoolTab) {
  activeTab.value = tab
  router.replace({ query: { ...route.query, tab } })
  if (tab === 'usage' && !usageLoaded.value) {
    fetchUsage()
  }
}
```

- `?tab=usage` 可以直接进来（刷新、分享、浏览器后退都对）。
- `usageLoaded` 保证只懒加载一次，来回切 tab 不重复请求。
- 账号池 tab 在 `onMounted` 里就加载（默认 tab）。

**注意时序**：直接 `goto('/pool?tab=usage')` 时，`fetchUsage` 会在组件挂载早期触发，
偶发首帧显示空态（「所选范围内没有号池使用记录」），下一个 tick 数据就到。
这不是 bug，是首帧渲染。真实交互（先点 tab）不会看到。

## 8. 两个已经修掉的显示 bug

### `endpoint` / `duration` 列永远显示「—」

`DataTable` 用 `row[column.key]` 取值。列 key 是 `endpoint` / `duration`，
而 DTO 字段是 `inbound_endpoint` / `duration_ms`，取不到 → 空。

修法是把这两列改成作用域插槽，手动取值：

```vue
<template #cell-endpoint="{ row }">
  {{ row.inbound_endpoint || row.upstream_endpoint || '—' }}
</template>

<template #cell-duration="{ row }">
  {{ row.duration_ms != null ? `${row.duration_ms} ms` : '—' }}
</template>
```

### 用户端账号显示「账号已删除」

`accountLabel(row)` 的取值优先级是：

```
row.account_name_snapshot  →  row.account?.name  →  t('admin.pool.usage.accountDeleted')
```

用户端 DTO 原来不带 `account_name_snapshot`（也不带 `account` 对象），
所以永远落到第三个分支 → 全部显示「账号已删除」，而账号明明还在。

修法是给用户端 DTO 补上 `account_name_snapshot`（只补名字，不补平台），见 `02-data-model.md`。

## 9. 请求流水抽屉

点表格里的「请求流水」按钮 → `BaseDialog`：

```ts
const requestsVisible = ref(false)
const activeAccount = ref<PoolAccount | null>(null)
const requestRows = ref<AdminUsageLog[]>([])
const requestPage = ref(1)
const requestPageSize = ref(10)

async function openRequests(row: PoolAccount) {
  activeAccount.value = row
  requestsVisible.value = true
  requestPage.value = 1
  await loadRequests()
}

async function loadRequests() {
  const res = await listPoolAccountRequests(
    activeAccount.value!.id,
    { page: requestPage.value, page_size: requestPageSize.value },
    scope.value
  )
  requestRows.value = res.items
  requestsTotal.value = res.total
}
```

抽屉 7 列：时间 / 模型 / 端点 / 输入·输出 / 费用 / 耗时 / 请求 ID。

## 10. 路由

```ts
// 管理端
{
  path: '/admin/pool',
  name: 'AdminPool',
  component: () => import('@/views/admin/PoolView.vue'),
  meta: {
    requiresAuth: true,
    requiresAdmin: true,
    title: 'Live Account Pool',
    titleKey: 'admin.pool.title',
    descriptionKey: 'admin.pool.description',
  },
},

// 用户端
{
  path: '/pool',
  name: 'Pool',
  component: () => import('@/views/admin/PoolView.vue'),
  meta: {
    requiresAuth: true,
    requiresAdmin: false,
    titleKey: 'admin.pool.user.title',
    descriptionKey: 'admin.pool.user.description',
  },
},
```

同一个组件文件，靠 `isAdmin` 分流。守卫逻辑在 `frontend/src/router/index.ts:901`：

```ts
if (requiresAdmin && !authStore.isAdmin) {
  next('/dashboard')
}
```

## 11. 侧边栏

```ts
// 管理端 adminNavItems 里
{
  path: '/admin/pool',
  label: t('admin.pool.title'),
  icon: DatabaseIcon,
}
// 注意：不再有 expandOnly: true 和 children
```

```ts
// 用户端 buildSelfNavItems 里，紧跟 /usage 之后
{
  path: '/pool',
  label: t('admin.pool.title'),
  icon: DatabaseIcon,
  hideInSimpleMode: true,
}
```

## 12. 菜单可见性

`frontend/src/config/menuPages.ts` 的 `menuPageGroups` 用于「菜单可见性」设置页，
必须登记新路径，否则管理员在设置里看不到这两项：

```ts
// admin 组：原来两条合并为一条
['/admin/pool', '实时号池', 'Live account pool'],

// user 组：在 /usage 之后新增
['/pool', '实时号池', 'Live account pool'],
```

## 13. i18n

zh / en 两个 locale 都要加，`check:i18n` 会校验 key 完整性
（`vitest run src/i18n/__tests__/localeKeyCompleteness.spec.ts`）。

新增的 key：

```
admin.pool.user.title                 我的号池用量
admin.pool.user.description           这里只展示你自己调用过的上游账号与用量，
                                      内部调度信息（分组、并发、倍率、优先级）不对普通用户开放。
admin.pool.user.empty                 你还没有使用过任何号池账号
admin.pool.user.overview.accounts     我用过的账号
admin.pool.user.overview.accountsHint 其中 {active} 个当前正常
admin.pool.usage.userTitle            我的号池使用记录
admin.pool.usage.userDescription      你在号池侧的全部请求明细。
                                      管理员为你指定的账号、模型映射与上游细节不会在此展示。
```

## 14. 已删除的旧页面

`PoolAccountsView.vue`（611 行）与 `PoolUsageView.vue`（367 行）已被 `PoolView.vue` 完全取代，
`grep -rn "PoolAccountsView\|PoolUsageView" frontend/src` 返回空。
这两个文件没有进版本库（`dev-gw` 分支不含它们），服务器磁盘上保留但无引用。