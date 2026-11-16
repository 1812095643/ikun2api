<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import UpstreamBalanceToolbar from './UpstreamBalanceToolbar.vue'
import UpstreamBalanceTable from './UpstreamBalanceTable.vue'
import UpstreamModelDialog from './UpstreamModelDialog.vue'
import UpstreamBalanceEditor from './UpstreamBalanceEditor.vue'
import UpstreamBalanceDetails from './UpstreamBalanceDetails.vue'
import UpstreamBalanceMonitoringDialog from './UpstreamBalanceMonitoringDialog.vue'
import { upstreamModelsAPI } from '@/api/upstreamModels'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { useUpstreamBalances } from '@/composables/useUpstreamBalances'
import { defaultBalanceDisplay, type BalanceColumn, type UpstreamBalance, type UpstreamBalanceDraft } from '@/api/upstreamBalances'

const { items, monitoring, loading, error, search, filter, visibility, page, pageSize, selectedIDs, refreshing, saving, filtered, visible, busy, hiddenCount, counts, display, displaySaving, displayReady, displayError, updateDisplay, load, refresh, save, setHidden, reorder, move, setDragging, resetFilters } = useUpstreamBalances()
const editing = shallowRef<UpstreamBalance | null>(null)
const showMonitoring = shallowRef(false)
const detailID = shallowRef('')
const modelID = shallowRef('')
const modelItem = computed(() => items.value.find(item => item.id === modelID.value) || null)
const details = computed(() => items.value.find(item => item.id === detailID.value) || null)
const modelBusy = shallowRef(false)
const app = useAppStore()
const { copyToClipboard } = useClipboard()
const emptyKind = computed(() => !items.value.length ? 'source' : visibility.value === 'hidden' && !hiddenCount.value ? 'hidden' : visibility.value === 'visible' && hiddenCount.value === items.value.length ? 'all-hidden' : 'filter')
function toggleColumn(key: BalanceColumn) {
  updateDisplay({ hidden_columns: display.value.hidden_columns.includes(key) ? display.value.hidden_columns.filter(column => column !== key) : [...display.value.hidden_columns, key] })
}
async function refreshModels() {
  if (modelBusy.value) return
  modelBusy.value = true
  try { await upstreamModelsAPI.refresh(); app.showSuccess('已加入模型检测队列'); await load(false) }
  catch { app.showError('无法发起模型检测') }
  finally { modelBusy.value = false }
}
async function handleSave(id: string, draft: UpstreamBalanceDraft) { if (await save(id, draft)) editing.value = null }
</script>

<template>
  <TablePageLayout class="upstream-page">
    <template #filters>
      <UpstreamBalanceToolbar v-model:search="search" v-model:filter="filter" v-model:visibility="visibility" :display="display" :total="items.length" :hidden-count="hiddenCount" :selected-count="selectedIDs.length" :result-count="filtered.length" :counts="counts" :loading="loading" :querying="!!refreshing" :model-busy="modelBusy" :display-saving="displaySaving" :ready="displayReady" @sort="updateDisplay({ sort_by: $event })" @toggle-column="toggleColumn" @compact="updateDisplay({ compact: !display.compact })" @reset-display="updateDisplay(defaultBalanceDisplay())" @reload="load()" @refresh="refresh(undefined, filtered.map(item => item.id))" @models="refreshModels" @monitoring="showMonitoring = true" @query-selected="refresh(undefined, selectedIDs)" @hide-selected="setHidden(selectedIDs, true)" @show-selected="setHidden(selectedIDs, false)" @clear-selection="selectedIDs = []" />
      <div v-if="error || displayError" role="alert" class="mt-3 flex items-center justify-between gap-3 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300"><span>{{ error || displayError }}</span><button v-if="error" type="button" class="shrink-0 underline" @click="load()">重试</button></div>
    </template>
    <template #table>
      <div class="upstream-table-heading"><div class="flex items-center gap-2"><span class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ visibility === 'hidden' ? '已隐藏上游' : '上游列表' }}</span><span class="text-xs tabular-nums text-gray-400 dark:text-dark-400">{{ filtered.length }}</span></div><span v-if="busy" role="status" class="inline-flex items-center gap-1.5 text-xs text-primary-600 dark:text-primary-400"><Icon name="refresh" size="xs" class="animate-spin" />后台查询中</span><span v-else class="text-[11px] text-gray-400 dark:text-dark-500">{{ display.sort_by === 'manual' ? '拖拽调整顺序' : '自动排序' }}</span></div>
      <UpstreamBalanceTable :items="visible" :loading="loading" :refreshing="refreshing" :hidden-ids="display.hidden" :hidden-columns="display.hidden_columns" :selected-ids="selectedIDs" :compact="display.compact" :can-reorder="displayReady && display.sort_by === 'manual'" :empty-kind="emptyKind" @edit="editing = $event" @details="detailID = $event.id" @refresh="refresh" @models="modelID = $event.id" @visibility="(id, hidden) => setHidden([id], hidden)" @reorder="reorder" @move="move" @dragging="setDragging" @select="selectedIDs = $event" @clear-filters="resetFilters" @show-hidden="visibility = 'hidden'" @copy="copyToClipboard($event, '上游地址已复制')" />
    </template>
    <template #pagination><Pagination v-model:page="page" v-model:page-size="pageSize" :total="filtered.length" :show-page-size-selector="true" /></template>
  </TablePageLayout>
  <UpstreamModelDialog :item="modelItem" @close="modelID = ''" @changed="load(false)" />
  <UpstreamBalanceMonitoringDialog v-if="showMonitoring" :initial="monitoring" :selected-ids="[...selectedIDs]" :total="items.length" @close="showMonitoring = false" @saved="load(false)" />
  <UpstreamBalanceEditor :item="editing" :saving="saving" @close="!saving && (editing = null)" @save="handleSave" />
  <UpstreamBalanceDetails :item="details" @close="detailID = ''" />
</template>

<style scoped>
.upstream-page { gap: 16px; }
.upstream-table-heading { @apply flex shrink-0 items-center justify-between gap-3 border-b border-gray-100 px-4 py-3 dark:border-dark-700; }
.upstream-page :deep(.table-scroll-container) { @apply rounded-xl; }
.upstream-page :deep(.table-scroll-container th) { @apply px-4 py-3 text-xs; }
.upstream-page :deep(.table-scroll-container td) { @apply px-4 py-5; }
.upstream-page :deep(.table-scroll-container .is-compact td) { @apply py-3; }
.upstream-page :deep(.table-scroll-container .row-controls) { @apply px-3; }
@media (max-width: 767px) {
  .upstream-table-heading { @apply border-0 px-0 pb-2 pt-0; }
  .upstream-page :deep(.table-scroll-container td),
  .upstream-page :deep(.table-scroll-container .is-compact td) { @apply border-0 p-0; }
  .upstream-page :deep(.table-scroll-container td.provider-cell),
  .upstream-page :deep(.table-scroll-container .is-compact td.provider-cell) { @apply px-7; }
  .upstream-page :deep(.table-scroll-container td.actions-cell),
  .upstream-page :deep(.table-scroll-container .is-compact td.actions-cell) { @apply border-t border-gray-100 pt-3 dark:border-dark-700; }
}
</style>
