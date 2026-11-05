<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores'
import { getPoolOverview, listPoolAccounts, listPoolUsage, type PoolAccount, type PoolOverview, type PoolUserOverview, type PoolScope, type PoolUsageQuery } from '@/api/pool'
import type { AdminUsageLog, UsageLog, PaginatedResponse } from '@/types'
import { getAll as listAdminGroups } from '@/api/admin/groups'
import { usePoolRequest } from '@/composables/usePoolRequest'
import { usePoolRefresh } from '@/composables/usePoolRefresh'
import { getPersistedPageSize, setPersistedPageSize } from '@/composables/usePersistedPageSize'
import { formatCostFixed, formatNumber, formatTokensK, formatTime } from '@/utils/format'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import Icon from '@/components/icons/Icon.vue'
import PoolAccountTable from './PoolAccountTable.vue'
import PoolUsageTable from './PoolUsageTable.vue'
import PoolRequestsDialog from './PoolRequestsDialog.vue'
import PoolDisplaySettings from './PoolDisplaySettings.vue'
import PoolNetworkError from './PoolNetworkError.vue'
import PoolOverviewStrip from './PoolOverviewStrip.vue'

const { t } = useI18n()
const route = useRoute(), router = useRouter(), auth = useAuthStore()
const isAdmin = computed(() => auth.isAdmin && route.path.startsWith('/admin/'))
const scope = computed<PoolScope>(() => isAdmin.value ? 'admin' : 'user')
type PoolTab = 'accounts' | 'usage'
const activeTab = computed<PoolTab>(() => route.query.tab === 'usage' ? 'usage' : 'accounts')
const tabItems = computed(() => [{ key: 'accounts' as const, label: t('admin.pool.navAccounts') }, { key: 'usage' as const, label: t('admin.pool.navUsage') }])
function switchTab(tab: PoolTab) { if (tab !== activeTab.value) void router.replace({ query: { ...route.query, tab } }) }
function tabKey(event: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const next = event.key === 'Home' ? 'accounts' : event.key === 'End' ? 'usage' : activeTab.value === 'accounts' ? 'usage' : 'accounts'
  switchTab(next)
  const group = (event.currentTarget as HTMLElement).parentElement
  ;(group?.querySelector(`[data-tab="${next}"]`) as HTMLElement | null)?.focus()
}
const accountsQuery = usePoolRequest<PaginatedResponse<PoolAccount> | null>(null)
const overviewQuery = usePoolRequest<(PoolOverview & PoolUserOverview) | null>(null)
const usageQuery = usePoolRequest<PaginatedResponse<AdminUsageLog & UsageLog> | null>(null)
const groupQuery = usePoolRequest<{ value: string; label: string }[]>([])
const { loading: accountsLoading, failed: accountsFailed } = accountsQuery
const { loading: overviewLoading, failed: overviewFailed } = overviewQuery
const { loading: usageLoading, failed: usageFailed } = usageQuery
const { failed: groupsFailed } = groupQuery
const overview = overviewQuery.data
const accounts = computed(() => accountsQuery.data.value?.items ?? [])
const accountsTotal = computed(() => accountsQuery.data.value?.total ?? 0)
const usageRows = computed(() => usageQuery.data.value?.items ?? [])
const usageTotal = computed(() => usageQuery.data.value?.total ?? 0)
const accountsPage = ref(1), accountsPageSize = ref(getPersistedPageSize(20))
const usagePage = ref(1), usagePageSize = ref(getPersistedPageSize(20))
const accountFilters = reactive({ search: '', platform: '', type: '', status: '', group_id: '' })
const appliedAccounts = ref({ ...accountFilters })
const groupOptions = computed(() => [{ value: '', label: t('admin.pool.filters.allGroups') }, ...groupQuery.data.value])
const activeAccount = ref<PoolAccount | null>(null)
const settingsVisible = ref(false)
const moreFilters = ref(false)
const dateString = (date: Date) => `${date.getFullYear()}-${String(date.getMonth()+1).padStart(2,'0')}-${String(date.getDate()).padStart(2,'0')}`
const now = new Date()
const usageStartDate = ref(dateString(new Date(now.getTime() - 86400000)))
const usageEndDate = ref(dateString(now))
const usageFilters = reactive({ account_id: '', user_id: '', api_key_id: '', model: '', request_id: '' })
const appliedUsage = ref<PoolUsageQuery>({})
const busy = computed(() => accountsLoading.value || overviewLoading.value || usageLoading.value)
const currentFailed = computed(() => activeTab.value === 'accounts' ? accountsFailed.value || overviewFailed.value : usageFailed.value)
const currentUpdatedAt = computed(() => activeTab.value === 'accounts' ? accountsQuery.updatedAt.value : usageQuery.updatedAt.value)

function fetchAccounts() {
  const filter = appliedAccounts.value, requestScope = scope.value
  return accountsQuery.run(signal => listPoolAccounts({ page: accountsPage.value, page_size: accountsPageSize.value, ...filter, group_id: isAdmin.value && filter.group_id ? Number(filter.group_id) : undefined }, requestScope, { signal }))
}
function fetchOverview() { const requestScope = scope.value; return overviewQuery.run(signal => getPoolOverview(requestScope, { signal })) }
async function refreshAccounts() { const results = await Promise.all([fetchAccounts(), fetchOverview()]); return results.every(Boolean) }
function searchAccounts() { accountsPage.value = 1; appliedAccounts.value = { ...accountFilters }; accountsQuery.reset(); void fetchAccounts() }
function resetAccountFilters() { Object.assign(accountFilters, { search:'', platform:'', type:'', status:'', group_id:'' }); searchAccounts() }
function onAccountsPageChange(page: number) { accountsPage.value = page; accountsQuery.reset(); void fetchAccounts() }
function onAccountsPageSizeChange(size: number) { accountsPageSize.value = size; setPersistedPageSize(size); onAccountsPageChange(1) }
function usageParams(): PoolUsageQuery {
  const result: PoolUsageQuery = { start_time: new Date(`${usageStartDate.value}T00:00:00`).toISOString(), end_time: new Date(`${usageEndDate.value}T23:59:59.999`).toISOString() }
  for (const key of ['account_id', 'api_key_id', ...(isAdmin.value ? ['user_id'] : [])] as const) {
    const raw = usageFilters[key as keyof typeof usageFilters], value = Number(raw)
    if (Number.isSafeInteger(value) && value > 0) Object.assign(result, { [key]: value })
  }
  if (usageFilters.model) result.model = usageFilters.model
  if (usageFilters.request_id) result.request_id = usageFilters.request_id
  return result
}
function fetchUsage() { const requestScope = scope.value; return usageQuery.run(signal => listPoolUsage({ ...appliedUsage.value, page: usagePage.value, page_size: usagePageSize.value }, requestScope, { signal })) }
function searchUsage() { appliedUsage.value = usageParams(); usagePage.value = 1; usageQuery.reset(); void fetchUsage() }
function resetUsageFilters() { Object.assign(usageFilters, { account_id:'', user_id:'', api_key_id:'', model:'', request_id:'' }); searchUsage() }
function onUsagePageChange(page: number) { usagePage.value = page; usageQuery.reset(); void fetchUsage() }
function onUsagePageSizeChange(size: number) { usagePageSize.value = size; setPersistedPageSize(size); onUsagePageChange(1) }
function loadGroupOptions() { if (!isAdmin.value) return; return groupQuery.run(async () => (await listAdminGroups()).map(g => ({ value: String(g.id), label: g.name }))) }
function refreshCurrent() { return activeTab.value === 'accounts' ? refreshAccounts() : fetchUsage() }
const { interval, paused, online } = usePoolRefresh(refreshCurrent, () => busy.value || !!activeAccount.value || settingsVisible.value)
const refreshOptions = computed(() => [ { value:'0', label:t('admin.pool.refreshOff') }, ...[10,30,60].map(seconds => ({ value:String(seconds), label:t('admin.pool.refreshEvery', { seconds }) })) ])
watch(scope, () => {
  accountsQuery.reset(); overviewQuery.reset(); usageQuery.reset(); groupQuery.reset()
  activeAccount.value = null; settingsVisible.value = false
  accountsPage.value = 1; usagePage.value = 1
  accountFilters.group_id = ''; appliedAccounts.value = { ...accountFilters }; appliedUsage.value = usageParams()
  void loadGroupOptions()
  if (activeTab.value === 'accounts') void refreshAccounts()
  else void fetchUsage()
}, { immediate: true })
watch(activeTab, tab => {
  accountsQuery.cancel(); overviewQuery.cancel(); usageQuery.cancel(); activeAccount.value = null
  if (tab === 'accounts') void refreshAccounts()
  else { appliedUsage.value = usageParams(); void fetchUsage() }
})
const platformOptions = computed(() => [
  { value: '', label: t('admin.pool.filters.allPlatform') },
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'openai_bps', label: 'OpenAI BPS' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' },
  { value: 'grok', label: 'Grok' },
  { value: 'kimi', label: 'Kimi' },
  { value: 'zhipu', label: '智谱 GLM' },
  { value: 'deepseek', label: 'DeepSeek' },
  { value: 'minimax', label: 'MiniMax' },
  { value: 'typesafe', label: 'TypeSafe' },
  { value: 'opencode_go', label: 'OpenCode' },
  { value: 'command_code', label: 'Command Code' }
])

const typeOptions = computed(() => [
  { value: '', label: t('admin.pool.filters.allType') },
  { value: 'oauth', label: 'OAuth' },
  { value: 'setup-token', label: 'Setup Token' },
  { value: 'apikey', label: 'API Key' },
  { value: 'upstream', label: 'Upstream' },
  { value: 'bedrock', label: 'Bedrock' },
  { value: 'service_account', label: 'Service Account' }
])

const statusOptions = computed(() => [
  { value: '', label: t('admin.pool.filters.allStatus') },
  { value: 'active', label: t('admin.pool.status.active') },
  { value: 'error', label: t('admin.pool.status.error') },
  { value: 'disabled', label: t('admin.pool.status.disabled') }
])

interface OverviewCard {
  key: string
  label: string
  value: string
  hint: string
}

const overviewCards = computed<OverviewCard[]>(() => {
  const o = overview.value
  const today = o?.today
  const week = o?.window_7d

  if (!isAdmin.value) {
    const accountsCount = o?.account_count ?? 0
    const activeCount = o?.active_count ?? 0
    return [
      {
        key: 'accounts',
        label: t('admin.pool.user.overview.accounts'),
        value: formatNumber(accountsCount),
        hint: t('admin.pool.user.overview.accountsHint', { active: activeCount })
      },
      {
        key: 'today_requests',
        label: t('admin.pool.overview.todayRequests'),
        value: formatNumber(today?.requests ?? 0),
        hint: formatTokensK(today?.tokens ?? 0)
      },
      {
        key: 'today_cost',
        label: t('admin.pool.overview.todayCost'),
        value: formatCostFixed(today?.user_cost ?? 0),
        hint: formatCostFixed(today?.standard_cost ?? 0)
      },
      {
        key: 'week_requests',
        label: t('admin.pool.overview.weekRequests'),
        value: formatNumber(week?.requests ?? 0),
        hint: t('admin.pool.overview.weekRequestsHint')
      }
    ]
  }

  return [
    {
      key: 'accounts',
      label: t('admin.pool.overview.accounts'),
      value: formatNumber(o?.total_accounts ?? 0),
      hint: t('admin.pool.overview.activeHint', {
        active: o?.active_accounts ?? 0,
        error: o?.error_accounts ?? 0,
        disabled: o?.disabled_accounts ?? 0
      })
    },
    {
      key: 'schedulable',
      label: t('admin.pool.overview.schedulable'),
      value: formatNumber(o?.schedulable_count ?? 0),
      hint: t('admin.pool.overview.schedulableHint')
    },
    {
      key: 'in_use',
      label: t('admin.pool.overview.inUse'),
      value: formatNumber(o?.total_in_use ?? 0),
      hint: t('admin.pool.overview.inUseHint')
    },
    {
      key: 'today_requests',
      label: t('admin.pool.overview.todayRequests'),
      value: formatNumber(today?.requests ?? 0),
      hint: formatTokensK(today?.tokens ?? 0)
    },
    {
      key: 'today_cost',
      label: t('admin.pool.overview.todayCost'),
      value: formatCostFixed(today?.cost ?? 0),
      hint: formatCostFixed(today?.standard_cost ?? 0)
    },
    {
      key: 'week_requests',
      label: t('admin.pool.overview.weekRequests'),
      value: formatNumber(week?.requests ?? 0),
      hint: t('admin.pool.overview.weekRequestsHint')
    }
  ]
})


</script>

<template>
  <TablePageLayout class="pool-page">
    <template #actions>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex items-center gap-1 rounded-xl bg-gray-100 p-1 dark:bg-dark-800" role="tablist" :aria-label="t('admin.pool.title')">
          <button v-for="tab in tabItems" :id="`pool-tab-${tab.key}`" :key="tab.key" type="button" role="tab" :data-tab="tab.key" :aria-selected="activeTab === tab.key" :aria-controls="`pool-panel-${tab.key}`" :tabindex="activeTab === tab.key ? 0 : -1" class="rounded-lg px-4 py-2 text-sm font-medium transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary-500" :class="activeTab === tab.key ? 'bg-white text-primary-600 shadow-sm dark:bg-dark-700 dark:text-primary-400' : 'text-gray-500 hover:text-gray-900 dark:text-dark-400 dark:hover:text-dark-100'" @click="switchTab(tab.key)" @keydown="tabKey">{{ tab.label }}</button>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <span class="hidden text-xs tabular-nums text-gray-500 dark:text-dark-400 2xl:inline" aria-live="polite">{{ !online ? t('admin.pool.networkError') : currentUpdatedAt ? t('admin.pool.updatedAt', { time: formatTime(new Date(currentUpdatedAt).toISOString()) }) : '' }}</span>
          <Select v-model="interval" class="w-36" :options="refreshOptions" :aria-label="t('admin.pool.autoRefresh')" />
          <button type="button" class="btn btn-secondary" :disabled="busy" :aria-label="t('common.refresh')" :title="paused ? t('admin.pool.refreshPaused') : t('common.refresh')" @click="refreshCurrent"><Icon name="refresh" size="md" :class="{ 'animate-spin': busy }" /></button>
          <button v-if="isAdmin" type="button" class="btn btn-secondary" @click="settingsVisible = true"><Icon name="cog" size="sm" />{{ t('admin.pool.displaySettings') }}</button>
        </div>
      </div>
    </template>
    <template #filters>
      <div class="space-y-3">
        <PoolNetworkError v-if="currentFailed || !online" :updated-at="currentUpdatedAt" :busy="busy" @retry="refreshCurrent" />
        <template v-if="activeTab === 'accounts'">
          <PoolOverviewStrip :items="overviewCards" :loading="overviewLoading" :unavailable="!overview" />
          <form class="flex flex-wrap items-center gap-3" @submit.prevent="searchAccounts">
            <div class="relative w-full sm:w-52">
              <Icon name="search" size="md" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
              <input v-model.trim="accountFilters.search" type="search" class="input pl-10" :aria-label="t('admin.pool.filters.search')" :placeholder="t('admin.pool.filters.searchPlaceholder')" />
            </div>
            <Select v-model="accountFilters.platform" class="w-[calc(50%-0.375rem)] sm:w-32" :options="platformOptions" :aria-label="t('admin.pool.filters.platform')" @change="searchAccounts" />
            <Select v-model="accountFilters.type" class="w-[calc(50%-0.375rem)] sm:w-32" :options="typeOptions" :aria-label="t('admin.pool.filters.type')" @change="searchAccounts" />
            <Select v-model="accountFilters.status" class="w-[calc(50%-0.375rem)] sm:w-32" :options="statusOptions" :aria-label="t('admin.pool.filters.status')" @change="searchAccounts" />
            <Select v-if="isAdmin" v-model="accountFilters.group_id" class="w-[calc(50%-0.375rem)] sm:w-36" :options="groupOptions" :disabled="groupsFailed" :aria-label="t('admin.pool.filters.group')" @change="searchAccounts" />
            <div class="ml-auto flex items-center gap-2">
              <button type="submit" class="btn btn-primary" :disabled="accountsLoading">{{ t('common.search') }}</button>
              <button type="button" class="btn btn-secondary" @click="resetAccountFilters">{{ t('common.reset') }}</button>
            </div>
          </form>
          <PoolNetworkError v-if="groupsFailed && isAdmin" @retry="loadGroupOptions" />
        </template>
        <form v-else class="space-y-3" @submit.prevent="searchUsage">
          <div class="flex flex-wrap items-center gap-3">
            <DateRangePicker v-model:start-date="usageStartDate" v-model:end-date="usageEndDate" @change="searchUsage" />
            <div class="relative w-full sm:w-52">
              <Icon name="search" size="md" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
              <input v-model.trim="usageFilters.model" class="input pl-10" :aria-label="t('admin.pool.usage.filters.model')" :placeholder="t('admin.pool.usage.filters.modelPlaceholder')" />
            </div>
            <button type="button" class="btn btn-secondary" :aria-expanded="moreFilters" aria-controls="pool-usage-filters" @click="moreFilters = !moreFilters"><Icon name="filter" size="sm" />{{ t('admin.pool.moreFilters') }}</button>
            <div class="ml-auto flex items-center gap-2">
              <button type="submit" class="btn btn-primary" :disabled="usageLoading">{{ t('common.search') }}</button>
              <button type="button" class="btn btn-secondary" @click="resetUsageFilters">{{ t('common.reset') }}</button>
            </div>
          </div>
          <div v-if="moreFilters" id="pool-usage-filters" class="grid grid-cols-1 gap-3 rounded-xl bg-gray-50 p-3 dark:bg-dark-800 sm:grid-cols-2 xl:grid-cols-4">
            <label class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.pool.usage.filters.accountId') }}<input v-model.trim="usageFilters.account_id" type="number" min="1" step="1" class="input mt-1" :placeholder="t('admin.pool.usage.filters.accountPlaceholder')" /></label>
            <label v-if="isAdmin" class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.pool.usage.filters.userId') }}<input v-model.trim="usageFilters.user_id" type="number" min="1" step="1" class="input mt-1" :placeholder="t('admin.pool.usage.filters.accountPlaceholder')" /></label>
            <label class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.pool.usage.filters.apiKeyId') }}<input v-model.trim="usageFilters.api_key_id" type="number" min="1" step="1" class="input mt-1" :placeholder="t('admin.pool.usage.filters.accountPlaceholder')" /></label>
            <label class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.pool.usage.filters.requestId') }}<input v-model.trim="usageFilters.request_id" class="input mt-1" :placeholder="t('admin.pool.usage.filters.requestIdPlaceholder')" /></label>
          </div>
        </form>
      </div>
    </template>
    <template #table>
      <div :id="`pool-panel-${activeTab}`" role="tabpanel" :aria-labelledby="`pool-tab-${activeTab}`" class="flex min-h-0 flex-1 flex-col" :aria-busy="busy">
        <PoolAccountTable v-if="activeTab === 'accounts'" :rows="accounts" :loading="accountsLoading" :failed="accountsFailed" :is-admin="isAdmin" @requests="activeAccount = $event" />
        <PoolUsageTable v-else :rows="usageRows" :loading="usageLoading" :failed="usageFailed" :is-admin="isAdmin" />
      </div>
    </template>
    <template #pagination>
      <Pagination v-if="activeTab === 'accounts' && accountsTotal > 0" :total="accountsTotal" :page="accountsPage" :page-size="accountsPageSize" @update:page="onAccountsPageChange" @update:page-size="onAccountsPageSizeChange" />
      <Pagination v-else-if="activeTab === 'usage' && usageTotal > 0" :total="usageTotal" :page="usagePage" :page-size="usagePageSize" @update:page="onUsagePageChange" @update:page-size="onUsagePageSizeChange" />
    </template>
  </TablePageLayout>
  <PoolRequestsDialog :account="activeAccount" :scope="scope" @close="activeAccount = null" />
  <PoolDisplaySettings v-if="isAdmin" :show="settingsVisible" @close="settingsVisible = false" @saved="refreshAccounts" />
</template>

<style scoped>
.pool-page { gap: 1rem; }
.pool-page :deep(.table-scroll-container th),
.pool-page :deep(.table-scroll-container td) { padding: 0.75rem 1rem; }
@media (max-width: 1023px) { .pool-page { height: auto; } }
</style>
