<script setup lang="ts">
import { computed } from 'vue'
import { VueDraggable } from 'vue-draggable-plus'
import Icon from '@/components/icons/Icon.vue'
import { balanceColumns, balanceComparisonLabel, balanceKindLabel, balanceScopeLabel, formatBalanceTime, formatUpstreamBalance, type BalanceColumn, type UpstreamBalance } from '@/api/upstreamBalances'

const props = withDefaults(defineProps<{
  items: UpstreamBalance[]; loading: boolean; refreshing: string | null
  hiddenIds?: string[]; hiddenColumns?: BalanceColumn[]; selectedIds?: string[]
  compact?: boolean; canReorder?: boolean; emptyKind?: 'source' | 'filter' | 'hidden' | 'all-hidden'
}>(), { hiddenIds: () => [], hiddenColumns: () => [], selectedIds: () => [], compact: true, canReorder: true, emptyKind: 'source' })
const emit = defineEmits<{
  edit: [item: UpstreamBalance]; details: [item: UpstreamBalance]; refresh: [id: string]; models: [item: UpstreamBalance]
  visibility: [id: string, hidden: boolean]; reorder: [ids: string[]]; move: [id: string, direction: number]
  dragging: [value: boolean]; select: [ids: string[]]; clearFilters: []; showHidden: []; copy: [url: string]
}>()
const columns = computed(() => balanceColumns.filter(col => !props.hiddenColumns.includes(col.key)))
const selected = computed(() => new Set(props.selectedIds))
const hidden = computed(() => new Set(props.hiddenIds))
const allSelected = computed(() => !!props.items.length && props.items.every(row => selected.value.has(row.id)))
const someSelected = computed(() => !allSelected.value && props.items.some(row => selected.value.has(row.id)))
const rows = computed({ get: () => props.items, set: value => emit('reorder', value.map(row => row.id)) })
const animation = typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 160
function select(ids: string[], checked: boolean) {
  const next = new Set(props.selectedIds)
  for (const id of ids) { if (checked) next.add(id); else next.delete(id) }
  emit('select', [...next])
}
function rowKind(row: UpstreamBalance) {
  const kinds = [...new Set(row.snapshot.readings.filter(reading => !reading.error).map(reading => reading.kind))]
  return balanceKindLabel(kinds.length === 1 ? kinds[0] : row.config.kind)
}
function failure(row: UpstreamBalance) { return row.snapshot.error || row.snapshot.readings.find(reading => reading.error)?.error || '' }
</script>

<template>
  <div class="upstream-table table-wrapper" :class="{ 'is-compact': compact }" :aria-busy="loading">
    <div v-if="items.length" class="mobile-select"><label><input type="checkbox" :checked="allSelected" :indeterminate="someSelected" @change="select(items.map(row => row.id), ($event.target as HTMLInputElement).checked)" />全选本页</label></div>
    <table class="w-full text-left" aria-label="上游余额列表">
      <thead><tr>
        <th scope="col" class="row-controls"><input type="checkbox" aria-label="全选本页上游" :checked="allSelected" :indeterminate="someSelected" :disabled="!items.length" @change="select(items.map(row => row.id), ($event.target as HTMLInputElement).checked)" /></th>
        <th scope="col" class="provider-heading">供应商</th><th scope="col" class="balance-heading">可用余额</th>
        <th v-for="column in columns" :key="column.key" scope="col">{{ column.label }}</th><th scope="col" class="actions-heading">操作</th>
      </tr></thead>
      <tbody v-if="loading && !items.length"><tr v-for="index in 5" :key="index"><td :colspan="columns.length + 4" class="skeleton-cell"><div class="h-11 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-700/60" /></td></tr></tbody>
      <VueDraggable v-else-if="items.length" v-model="rows" tag="tbody" :animation="animation" handle=".upstream-drag-handle" :disabled="!canReorder || loading" :force-fallback="true" :delay-on-touch-only="true" :delay="120" ghost-class="upstream-drag-ghost" chosen-class="upstream-drag-chosen" @start="emit('dragging', true)" @end="emit('dragging', false)">
        <tr v-for="row in rows" :key="row.id" :data-row-id="row.id" :class="{ 'row-hidden': hidden.has(row.id), 'row-selected': selected.has(row.id) }">
          <td class="row-controls">
            <input type="checkbox" :aria-label="`选择 ${row.name}`" :checked="selected.has(row.id)" @change="select([row.id], ($event.target as HTMLInputElement).checked)" />
            <button type="button" class="upstream-drag-handle" :disabled="!canReorder" :aria-label="`调整 ${row.name} 的顺序`" :title="canReorder ? '拖拽排序，也可按 ↑ / ↓ 移动' : '切换手动排序后可拖拽'" @keydown.up.prevent="emit('move', row.id, -1)" @keydown.down.prevent="emit('move', row.id, 1)"><svg viewBox="0 0 20 20" class="h-4 w-4" fill="currentColor" aria-hidden="true"><circle cx="7" cy="5" r="1.2" /><circle cx="13" cy="5" r="1.2" /><circle cx="7" cy="10" r="1.2" /><circle cx="13" cy="10" r="1.2" /><circle cx="7" cy="15" r="1.2" /><circle cx="13" cy="15" r="1.2" /></svg></button>
          </td>
          <td class="provider-cell">
            <div class="flex min-w-0 items-center gap-2"><a :href="row.url" target="_blank" rel="noopener noreferrer" class="provider-name" :title="row.name">{{ row.name }}</a><span v-if="hidden.has(row.id)" class="shrink-0 rounded bg-gray-100 px-1.5 py-0.5 text-[10px] text-gray-500 dark:bg-dark-700 dark:text-dark-400">已隐藏</span><span class="provider-kind">{{ rowKind(row) }}</span></div>
            <div class="provider-url"><a :href="row.url" target="_blank" rel="noopener noreferrer" :title="row.url" class="truncate hover:text-primary-600 dark:hover:text-primary-400">{{ row.url.replace(/^https?:\/\//, '').replace(/\/$/, '') }}</a><button type="button" class="copy-url" :aria-label="`复制 ${row.name} 的地址`" title="复制地址" @click="emit('copy', row.url)"><Icon name="copy" size="xs" /></button><span class="provider-accounts">{{ row.accounts.length }} 账号<span>·</span>{{ row.key_count }} Key</span></div>
          </td>
          <td data-label="可用余额" class="balance-cell"><button type="button" class="balance-value" :class="{ 'balance-low': row.alert.low }" @click="emit('details', row)">{{ formatUpstreamBalance(row.snapshot.balance, row.snapshot.unit, row.snapshot.unlimited) }}</button><p v-if="row.alert.low" class="text-xs text-red-500">低于提醒阈值</p><p v-else-if="failure(row) && row.snapshot.balance == null" class="balance-note text-amber-600 dark:text-amber-400" :title="failure(row)">查询异常</p><p v-else class="balance-note">{{ row.snapshot.scope ? balanceScopeLabel(row.snapshot.scope) : row.last_checked_at ? '暂无读数' : '尚未查询' }}</p></td>
          <td v-if="!hiddenColumns.includes('comparison')" data-label="Key 校验">
            <span v-if="row.checking || row.queued" class="status-badge text-primary-600 dark:text-primary-400"><Icon :name="row.checking ? 'refresh' : 'clock'" size="xs" :class="{ 'animate-spin': row.checking }" />{{ row.checking ? '查询中' : '已排队' }}</span>
            <button v-else-if="row.last_checked_at" type="button" class="status-badge" :class="['different','partial','unverified'].includes(row.snapshot.comparison) ? 'status-warning' : row.snapshot.comparison === 'consistent' ? 'status-success' : 'status-neutral'" @click="emit('details', row)"><span class="status-dot" />{{ row.token_configured && row.config.kind !== 'sub2api' && row.snapshot.total === 1 ? '钱包令牌' : balanceComparisonLabel(row.snapshot.comparison) }}</button>
            <span v-else class="muted">未校验</span>
            <p v-if="row.last_checked_at" class="cell-detail">{{ row.snapshot.successful }} / {{ row.snapshot.total }} 成功</p>
          </td>
          <td v-if="!hiddenColumns.includes('monitor')" data-label="余额监控"><button type="button" class="text-left" @click="emit('edit', row)"><span class="inline-flex items-center gap-1.5 text-xs" :class="row.config.monitor_enabled ? 'text-gray-700 dark:text-gray-200' : 'muted'"><span class="status-dot" :class="row.config.monitor_enabled ? 'bg-primary-500' : 'bg-gray-300 dark:bg-dark-500'" />{{ row.config.monitor_enabled ? '监控中' : '未启用' }}</span><span v-if="row.config.monitor_enabled" class="cell-detail">{{ row.config.interval_minutes }} 分钟 · 低于 {{ row.config.threshold }}</span><span v-if="row.alert.error" class="cell-detail text-amber-600 dark:text-amber-400" :title="row.alert.error">{{ row.alert.error.includes('邮件') ? '邮件发送异常' : '检查监控来源' }}</span></button></td>
          <td v-if="!hiddenColumns.includes('models')" data-label="模型变更"><button type="button" class="text-left" @click="emit('models', row)"><span v-if="row.models?.checking || row.models?.queued" class="text-xs text-primary-600 dark:text-primary-400">{{ row.models.checking ? '检测中' : '已排队' }}</span><span v-else-if="row.models?.pending" class="status-badge status-warning">{{ row.models.pending }} 项待处理<Icon name="chevronRight" size="xs" /></span><span v-else-if="row.models?.errors" class="text-xs text-amber-600 dark:text-amber-400">检测异常</span><span v-else class="muted hover:text-primary-600 dark:hover:text-primary-400">{{ row.models?.checked_at ? '无变更' : '未检测' }}</span><span class="cell-detail">{{ row.models?.errors ? `${row.models.errors} 个异常` : row.models?.enabled ? '定时检测中' : '查看目录' }}</span></button></td>
          <td v-if="!hiddenColumns.includes('last_checked_at')" data-label="最近查询"><span class="whitespace-nowrap text-xs tabular-nums text-gray-600 dark:text-dark-300" :title="row.last_checked_at || ''">{{ formatBalanceTime(row.last_checked_at) }}</span><p v-if="row.config.monitor_enabled && row.next_check_at" class="cell-detail">下次 {{ formatBalanceTime(row.next_check_at) }}</p></td>
          <td class="actions-cell"><div class="flex items-center gap-1"><button type="button" class="query-button" :disabled="!!refreshing || row.checking || row.queued" @click="emit('refresh', row.id)">{{ row.checking ? '查询中' : row.queued ? '已排队' : '查询' }}</button><button type="button" class="row-action" :aria-label="`查看 ${row.name} 的 Key 明细`" title="Key 明细" @click="emit('details', row)"><Icon name="list" size="sm" /></button><button type="button" class="row-action" :aria-label="`设置 ${row.name}`" title="上游设置" @click="emit('edit', row)"><Icon name="cog" size="sm" /></button><button type="button" class="row-action" :aria-label="`${hidden.has(row.id) ? '显示' : '隐藏'} ${row.name}`" :title="hidden.has(row.id) ? '恢复显示' : '从列表隐藏'" @click="emit('visibility', row.id, !hidden.has(row.id))"><Icon :name="hidden.has(row.id) ? 'eye' : 'eyeOff'" size="sm" /></button></div></td>
        </tr>
      </VueDraggable>
      <tbody v-else><tr><td :colspan="columns.length + 4" class="empty-cell"><Icon :name="emptyKind === 'hidden' ? 'eyeOff' : 'server'" size="xl" class="mx-auto mb-3 text-gray-300 dark:text-dark-500" /><p class="font-medium text-gray-700 dark:text-gray-200">{{ emptyKind === 'source' ? '暂无 API Key 上游' : emptyKind === 'hidden' ? '没有隐藏的上游' : emptyKind === 'all-hidden' ? '上游已全部隐藏' : '没有匹配的上游' }}</p><RouterLink v-if="emptyKind === 'source'" to="/admin/accounts" class="btn btn-secondary mt-4">账号管理</RouterLink><button v-else-if="emptyKind === 'all-hidden'" type="button" class="mt-3 text-sm text-primary-600 dark:text-primary-400" @click="emit('showHidden')">查看已隐藏</button><button v-else-if="emptyKind === 'filter'" type="button" class="mt-3 text-sm text-primary-600 dark:text-primary-400" @click="emit('clearFilters')">清除筛选</button></td></tr></tbody>
    </table>
  </div>
</template>

<style scoped>
.upstream-table { @apply h-full overflow-auto; scrollbar-gutter: stable; }
table { @apply min-w-max border-collapse; }
thead { @apply sticky top-0 z-10 bg-gray-50 dark:bg-dark-800; }
th { @apply whitespace-nowrap border-b border-gray-200 px-4 py-3 text-xs font-medium text-gray-500 dark:border-dark-700 dark:text-dark-400; }
td { @apply border-b border-gray-100 px-4 py-5 align-middle text-sm text-gray-800 dark:border-dark-800 dark:text-gray-200; }
tbody tr { @apply bg-white transition-colors hover:bg-gray-50/80 dark:bg-dark-900 dark:hover:bg-dark-800/70; }
tbody tr:last-child td { @apply border-b-0; }
.is-compact td { @apply py-3; }
.row-controls { @apply w-16 min-w-16 whitespace-nowrap px-3; }
.row-controls input, .mobile-select input { @apply h-3.5 w-3.5 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800; }
.upstream-drag-handle { @apply ml-2 inline-flex cursor-grab items-center rounded py-1 text-gray-300 hover:text-gray-600 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 active:cursor-grabbing disabled:cursor-default disabled:opacity-30 dark:text-dark-500 dark:hover:text-dark-200; touch-action: none; }
.provider-heading { @apply min-w-56; }
.provider-cell { @apply min-w-56 max-w-xs; }
.provider-name { @apply min-w-0 truncate font-semibold text-gray-900 hover:text-primary-600 dark:text-gray-100 dark:hover:text-primary-400; max-width: 220px; }
.provider-kind { @apply shrink-0 rounded bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium text-gray-500 dark:bg-dark-800 dark:text-dark-400; }
.provider-url { @apply mt-1.5 flex max-w-72 items-center gap-1 text-[11px] text-gray-400 dark:text-dark-400; }
.copy-url { @apply shrink-0 rounded p-0.5 text-gray-300 hover:bg-gray-100 hover:text-gray-700 focus-visible:outline focus-visible:outline-primary-500 dark:text-dark-500 dark:hover:bg-dark-700 dark:hover:text-gray-200; }
.provider-accounts { @apply ml-1 shrink-0 text-[10px] tabular-nums text-gray-400 dark:text-dark-500; }
.provider-accounts span { @apply px-1; }
.balance-heading, .balance-cell { @apply min-w-36; }
.balance-value { @apply whitespace-nowrap text-lg font-semibold tabular-nums text-gray-900 hover:underline dark:text-gray-100; }
.balance-value.balance-low { @apply text-red-600 dark:text-red-400; }
.balance-note { @apply mt-0.5 text-[11px] text-gray-400 dark:text-dark-400; }
.status-badge { @apply inline-flex items-center gap-1.5 whitespace-nowrap rounded-md px-1.5 py-1 text-[11px] font-medium; }
.status-dot { @apply inline-block h-1.5 w-1.5 shrink-0 rounded-full; }
.status-badge .status-dot { background-color: currentColor; }
.status-success { @apply bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300; }
.status-warning { @apply bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300; }
.status-neutral { @apply bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-dark-300; }
.cell-detail { @apply mt-1 block whitespace-nowrap text-[11px] text-gray-400 dark:text-dark-400; }
.muted { @apply text-xs text-gray-400 dark:text-dark-500; }
.actions-cell { @apply w-px whitespace-nowrap; }
.query-button { @apply mr-1 inline-flex h-7 min-w-12 items-center justify-center rounded-lg border border-gray-200 px-2 text-xs font-medium text-gray-600 hover:border-primary-300 hover:bg-primary-50 hover:text-primary-700 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 disabled:cursor-not-allowed disabled:opacity-40 dark:border-dark-600 dark:text-gray-300 dark:hover:border-primary-700 dark:hover:bg-primary-900/20 dark:hover:text-primary-300; }
.row-action { @apply inline-flex h-7 w-7 items-center justify-center rounded-lg text-gray-400 hover:bg-gray-100 hover:text-gray-800 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:text-dark-400 dark:hover:bg-dark-700 dark:hover:text-gray-100; }
.row-hidden .provider-cell, .row-hidden .balance-cell { opacity: .65; }
tbody tr.row-selected { @apply bg-primary-50/60 dark:bg-primary-900/15; }
.skeleton-cell { @apply py-3; }
.empty-cell { @apply py-20 text-center; }
.mobile-select { @apply hidden; }
:deep(.upstream-drag-ghost) { @apply bg-primary-50 opacity-40 dark:bg-primary-900/20; }
:deep(.upstream-drag-chosen) { @apply bg-primary-50/50 dark:bg-primary-900/15; }
@media (max-width: 767px) {
  .upstream-table { @apply overflow-visible; }
  table, tbody { @apply block min-w-0; }
  thead { @apply hidden; }
  tbody tr { @apply relative mb-3 grid grid-cols-2 gap-x-4 gap-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700; }
  td { @apply block min-w-0 border-0 p-0; }
  .is-compact td { @apply p-0; }
  .row-controls { @apply absolute left-4 right-4 top-4 flex w-auto justify-between p-0; pointer-events: none; }
  .row-controls > * { pointer-events: auto; }
  .upstream-drag-handle { @apply -mt-1 p-1; }
  .provider-cell { @apply col-span-2 max-w-none px-7; }
  .provider-name { max-width: none; }
  .provider-url { @apply max-w-none; }
  .provider-kind { @apply hidden; }
  td[data-label]::before { content: attr(data-label); @apply mb-1.5 block text-[11px] font-medium text-gray-400 dark:text-dark-400; }
  .balance-value { @apply text-xl; }
  .actions-cell { @apply col-span-2 w-full border-t border-gray-100 pt-3 dark:border-dark-700; }
  .actions-cell > div { @apply justify-end; }
  .row-action { @apply h-9 w-9; }
  .query-button { @apply h-9 min-w-16; }
  .mobile-select { @apply mb-3 flex justify-end text-xs text-gray-500 dark:text-dark-400; }
  .mobile-select label { @apply inline-flex items-center gap-2; }
  .empty-cell, .skeleton-cell { @apply col-span-2 w-full py-12; }
}
@media (prefers-reduced-motion: reduce) { tbody tr { transition: none; } }
</style>
