<script setup lang="ts">
import { onBeforeUnmount, onMounted, shallowRef } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BlindboxRecords from './BlindboxRecords.vue'
import { blindboxAPI, type BlindboxRound, type BlindboxEntryPage, type BlindboxRoundPage } from '@/api/blindbox'
import { checkinMoney } from '@/api/checkin'
import { workspaceError } from '@/api/workspace'
const rounds = shallowRef<BlindboxRoundPage | null>(null), entries = shallowRef<BlindboxEntryPage | null>(null)
const selected = shallowRef<BlindboxRound | null>(null), page = shallowRef(1)
const loading = shallowRef(false), detailLoading = shallowRef(false), error = shallowRef(''), detailError = shallowRef('')
let revision = 0, listRevision = 0, alive = true
const columns = [{ key: 'date', label: '场次' }, { key: 'status', label: '状态' }, { key: 'count', label: '份数' }, { key: 'total_paid', label: '奖池' }, { key: 'total_awarded', label: '已发奖励' }, { key: 'difference', label: '结算差额' }, { key: 'actions', label: '' }]
async function load(nextPage = page.value) {
  const request = ++listRevision
  loading.value = true
  try {
    const result = await blindboxAPI.rounds(nextPage)
    if (!alive || request !== listRevision) return
    rounds.value = result; page.value = nextPage; error.value = ''
  } catch (e) { if (alive && request === listRevision) error.value = workspaceError(e) }
  finally { if (alive && request === listRevision) loading.value = false }
}
async function detail(round: BlindboxRound, nextPage = 1) {
  const request = ++revision
  if (selected.value?.id !== round.id) entries.value = null
  selected.value = round; detailLoading.value = true; detailError.value = ''
  try {
    const result = await blindboxAPI.entries(nextPage, true, round.id)
    if (alive && request === revision) entries.value = result
  } catch (e) { if (alive && request === revision) detailError.value = workspaceError(e) }
  finally { if (alive && request === revision) detailLoading.value = false }
}
function close() { revision++; selected.value = null; entries.value = null; detailLoading.value = false }
onMounted(() => { void load() })
onBeforeUnmount(() => { alive = false; revision++; listRevision++ })
</script>
<template>
  <section class="space-y-4">
    <div class="flex items-center justify-between gap-3"><h2 class="text-base font-semibold text-gray-900 dark:text-gray-100">场次与账目</h2><button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="load()"><Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />刷新</button></div>
    <p v-if="error" class="text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</p>
    <DataTable :columns="columns" :data="rounds?.items || []" :loading="loading">
      <template #cell-date="{ row }"><span class="font-medium">{{ row.date }}</span><span class="ml-2 text-xs text-gray-400">#{{ row.id }}</span></template>
      <template #cell-status="{ value }"><span class="rounded px-2 py-1 text-xs" :class="value === 'settled' ? 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300' : 'bg-blue-50 text-blue-600 dark:bg-blue-500/10 dark:text-blue-400'">{{ value === 'settled' ? '已结算' : '参与中' }}</span></template>
      <template #cell-total_paid="{ value }">{{ checkinMoney(value) }}</template>
      <template #cell-total_awarded="{ value }">{{ checkinMoney(value) }}</template>
      <template #cell-difference="{ row }">{{ row.status === 'settled' ? checkinMoney((Number(row.total_paid) - Number(row.total_awarded)).toFixed(2)) : '待结算' }}</template>
      <template #cell-actions="{ row }"><button type="button" class="btn btn-secondary btn-sm" @click="detail(row)">查看明细</button></template>
      <template #empty><p class="py-5 text-sm text-gray-500">暂无盲盒场次</p></template>
    </DataTable>
    <Pagination v-if="rounds && rounds.total > rounds.page_size" :total="rounds.total" :page="rounds.page" :page-size="rounds.page_size" :show-page-size-selector="false" @update:page="load" />
    <BaseDialog :show="!!selected" :title="`${selected?.date || ''} · 盲盒明细`" width="wide" content-class="blindbox-record-dialog" @close="close">
      <template v-if="selected">
        <div class="mb-4 flex flex-wrap gap-x-6 gap-y-2 border-b border-gray-100 pb-3 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400"><span>参与 <b class="text-gray-800 dark:text-gray-100">{{ selected.count }}</b> 份</span><span>奖池 <b class="text-gray-800 dark:text-gray-100">{{ checkinMoney(selected.total_paid) }}</b></span><span>奖励 <b class="text-gray-800 dark:text-gray-100">{{ checkinMoney(selected.total_awarded) }}</b></span><span>{{ selected.status === 'settled' ? '已结算' : `${selected.spec.draw_time} 开奖` }}</span></div>
        <p v-if="detailError" class="mb-3 text-sm text-red-600 dark:text-red-400" role="alert">{{ detailError }} <button type="button" class="underline" @click="detail(selected)">重试</button></p>
        <BlindboxRecords v-if="entries" :history="entries" :loading="detailLoading" admin @page="detail(selected!, $event)" />
        <p v-else-if="detailLoading" class="py-10 text-center text-sm text-gray-500" role="status">读取明细中…</p>
      </template>
      <template #footer><button type="button" class="btn btn-secondary btn-sm" :disabled="detailLoading || !selected" @click="selected && detail(selected, entries?.page || 1)"><Icon name="refresh" size="sm" />刷新明细</button><button type="button" class="btn btn-primary btn-sm" @click="close">关闭</button></template>
    </BaseDialog>
  </section>
</template>
<style>
.blindbox-record-dialog { max-width: 54rem !important; }
.blindbox-record-dialog .modal-header,.blindbox-record-dialog .modal-body { padding: 16px 20px; }
.blindbox-record-dialog .table-container { max-height: 48dvh; overflow: auto; }
.blindbox-record-dialog td,.blindbox-record-dialog th { padding-block: 10px; }
</style>
