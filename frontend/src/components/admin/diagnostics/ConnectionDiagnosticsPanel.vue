<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import DataTable from '@/components/common/DataTable.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import ConnectionTimeline from './ConnectionTimeline.vue'
import { useConnectionDiagnostics } from '@/composables/useConnectionDiagnostics'
import type { ConnectionQuery, ConnectionTrace } from '@/api/admin/connectionDiagnostics'

const { t, te } = useI18n(), route = useRoute()
const { result, selected, loading, detailLoading, error, detailError, cursors, load, open, close } = useConnectionDiagnostics()
const localTime = (at: number) => new Date(at - new Date(at).getTimezoneOffset() * 60000).toISOString().slice(0, 16)
const start = ref(localTime(Date.now() - 3600000)), end = ref(localTime(Date.now() + 60000))
const requestId = ref(String(route.query.request_id || '')), accountId = ref(String(route.query.account_id || '')), userId = ref(String(route.query.user_id || '')), groupId = ref(String(route.query.group_id || ''))
const model = ref(''), state = ref(''), transport = ref(''), validation = ref(false)
const appliedQuery = ref<ConnectionQuery | null>(null)
const states = ['completed', 'failed', 'client_disconnected', 'downstream_write_failed', 'incomplete', 'unknown', 'websocket_closed']
const stateOptions = computed(() => [{ value: '', label: t('connections.all') }, ...states.map(value => ({ value, label: t(`connections.states.${value}`) }))])
const transportOptions = computed(() => [{ value: '', label: t('connections.all') }, ...['http', 'websocket'].map(value => ({ value, label: t(`connections.${value}`) }))])
const columns = computed(() => [
  { key: 'started_at', label: t('connections.time') }, { key: 'request_id', label: t('connections.request') }, { key: 'account_id', label: t('connections.account') }, { key: 'model', label: t('connections.model') }, { key: 'state', label: t('connections.state') }, { key: 'first_byte_ms', label: t('connections.firstByte') }, { key: 'first_token_ms', label: t('connections.firstToken') }, { key: 'total_ms', label: t('connections.duration') }
])
const ms = (value: number | null) => value == null ? '—' : value >= 1000 ? `${(value / 1000).toFixed(2)}s` : `${Math.round(value)}ms`
const stateLabel = (value: string) => te(`connections.states.${value}`) ? t(`connections.states.${value}`) : t('connections.states.unknown')
function search() {
  const from = new Date(start.value), to = new Date(end.value)
  validation.value = !Number.isFinite(from.getTime()) || !Number.isFinite(to.getTime()) || from >= to || to.getTime() - from.getTime() > 7 * 86400000
  if (validation.value) return
  appliedQuery.value = { start_time: from.toISOString(), end_time: to.toISOString(), request_id: requestId.value.trim() || undefined, account_id: Number(accountId.value) || undefined, user_id: Number(userId.value) || undefined, group_id: Number(groupId.value) || undefined, model: model.value.trim() || undefined, state: state.value || undefined, transport: transport.value || undefined }
  void load(appliedQuery.value)
}
function page(direction: 'previous' | 'next') { if (appliedQuery.value) void load(appliedQuery.value, direction) }
function openTrace(row: ConnectionTrace) { void open(row) }
onMounted(search)
</script>

<template>
  <section class="space-y-4">
    <div class="flex items-center justify-between gap-3"><h1 class="text-xl font-semibold tracking-tight">{{ t('connections.title') }}</h1><span v-if="result && (result.health.dropped || result.health.write_failed)" class="text-xs text-amber-600">{{ t('connections.dropped', { count: result.health.dropped + result.health.write_failed }) }}</span></div>
    <form class="card space-y-3 p-3 sm:p-4" @submit.prevent="search">
      <div class="flex flex-wrap items-end gap-2">
        <label class="min-w-0 flex-1 space-y-1 text-xs text-gray-500 sm:flex-none">{{ t('connections.start') }}<input v-model="start" type="datetime-local" class="input w-full sm:w-48" /></label>
        <label class="min-w-0 flex-1 space-y-1 text-xs text-gray-500 sm:flex-none">{{ t('connections.end') }}<input v-model="end" type="datetime-local" class="input w-full sm:w-48" /></label>
        <label class="min-w-48 flex-1 space-y-1 text-xs text-gray-500">{{ t('connections.request') }}<input v-model="requestId" class="input w-full font-mono" maxlength="128" /></label>
        <button type="submit" class="btn btn-primary shrink-0" :disabled="loading"><Icon name="search" size="sm" /><span>{{ t('connections.search') }}</span></button>
      </div>
      <div class="grid grid-cols-2 gap-2 sm:grid-cols-3 xl:grid-cols-6">
        <label class="space-y-1 text-xs text-gray-500">{{ t('connections.state') }}<Select v-model="state" :options="stateOptions" :aria-label="t('connections.state')" /></label>
        <label class="space-y-1 text-xs text-gray-500">{{ t('connections.transport') }}<Select v-model="transport" :options="transportOptions" :aria-label="t('connections.transport')" /></label>
        <label class="space-y-1 text-xs text-gray-500">{{ t('connections.model') }}<input v-model="model" class="input w-full" maxlength="256" /></label>
        <label class="space-y-1 text-xs text-gray-500">{{ t('connections.account') }}<input v-model="accountId" type="number" min="1" class="input w-full" /></label>
        <label class="space-y-1 text-xs text-gray-500">{{ t('connections.user') }}<input v-model="userId" type="number" min="1" class="input w-full" /></label>
        <label class="space-y-1 text-xs text-gray-500">{{ t('connections.group') }}<input v-model="groupId" type="number" min="1" class="input w-full" /></label>
      </div>
      <p v-if="validation" role="alert" class="text-xs text-red-600">{{ t('connections.invalidRange') }}</p>
    </form>
    <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ t('connections.loadError') }}</p>
    <DataTable :data="result?.items ?? []" :columns="columns" :loading="loading" row-key="trace_id" clickable-rows @row-click="openTrace">
      <template #cell-started_at="{ row }"><span class="whitespace-nowrap text-xs tabular-nums">{{ new Date(row.started_at).toLocaleString() }}</span></template>
      <template #cell-request_id="{ row }"><button type="button" class="max-w-44 truncate text-left font-mono text-xs text-primary-600 hover:underline dark:text-primary-400" :title="row.request_id || row.trace_id" @click.stop="openTrace(row)">{{ row.request_id || row.trace_id }}</button><p class="mt-0.5 text-[11px] text-gray-400">{{ row.transport === 'websocket' ? 'WS' : 'HTTP' }} · {{ row.path }}</p></template>
      <template #cell-account_id="{ row }"><span class="tabular-nums">{{ row.account_id || '—' }}</span></template>
      <template #cell-model="{ row }"><span class="text-xs">{{ row.model || '—' }}</span></template>
      <template #cell-state="{ row }"><span class="inline-flex rounded-md px-2 py-1 text-xs" :class="row.state === 'completed' || row.state === 'websocket_closed' ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300' : row.state === 'unknown' ? 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300' : 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300'">{{ stateLabel(row.state) }}</span></template>
      <template #cell-first_byte_ms="{ row }"><span class="tabular-nums">{{ ms(row.first_byte_ms) }}</span></template>
      <template #cell-first_token_ms="{ row }"><span class="tabular-nums" :class="row.first_token_ms >= 5000 ? 'text-amber-600' : ''">{{ ms(row.first_token_ms) }}</span></template>
      <template #cell-total_ms="{ row }"><span class="font-medium tabular-nums">{{ ms(row.total_ms) }}</span></template>
      <template #empty><p class="py-8 text-sm text-gray-500">{{ t('connections.empty') }}</p></template>
    </DataTable>
    <div class="flex items-center justify-end gap-3"><span class="text-xs tabular-nums text-gray-500">{{ t('connections.page', { page: cursors.length }) }}</span><button type="button" class="btn btn-secondary btn-sm" :disabled="loading || cursors.length <= 1" @click="page('previous')">{{ t('connections.previous') }}</button><button type="button" class="btn btn-secondary btn-sm" :disabled="loading || !result?.next_cursor" @click="page('next')">{{ t('connections.next') }}</button></div>
    <BaseDialog :show="!!selected" :title="t('connections.detail')" width="wide" @close="close">
      <p v-if="detailLoading" class="py-8 text-center text-sm text-gray-500">{{ t('common.loading') }}</p>
      <p v-else-if="detailError" role="alert" class="text-sm text-red-600">{{ t('connections.loadError') }}</p>
      <ConnectionTimeline v-else-if="selected" :row="selected" />
    </BaseDialog>
  </section>
</template>
