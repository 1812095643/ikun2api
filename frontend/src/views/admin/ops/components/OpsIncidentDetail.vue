<script setup lang="ts">
import { computed, onBeforeUnmount, shallowRef, watch } from 'vue'
import { useDocumentVisibility } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import ConnectionTimeline from '@/components/admin/diagnostics/ConnectionTimeline.vue'
import { listConnectionTraces, getConnectionTrace, type ConnectionTrace } from '@/api/admin/connectionDiagnostics'
import { opsMonitorAPI, type MonitorDetail, type MonitorRelatedRequest } from '@/api/admin/opsMonitor'
import OpsErrorDetailsModal from './OpsErrorDetailsModal.vue'
import OpsErrorDetailModal from './OpsErrorDetailModal.vue'
import OpsDeliveryTable from './OpsDeliveryTable.vue'
import OpsIncidentStatus from './OpsIncidentStatus.vue'
import OpsIncidentRequests from './OpsIncidentRequests.vue'
import OpsIncidentHistory from './OpsIncidentHistory.vue'
import { incidentActive, incidentSilenced, incidentTime, singleRequestWait } from '../utils/incidentPresentation'

const props = defineProps<{ id: string; refreshToken?: number }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const { t, te } = useI18n(), visibility = useDocumentVisibility()
const detail = shallowRef<MonitorDetail | null>(null), loading = shallowRef(false), busy = shallowRef(false), error = shallowRef('')
const stale = shallowRef(false)
const trace = shallowRef<ConnectionTrace | null>(null), traces = shallowRef<ConnectionTrace[]>([]), traceID = shallowRef(''), traceState = shallowRef('')
const showErrors = shallowRef(false), errorID = shallowRef<number | null>(null), errorDetail = shallowRef(false), errorType = shallowRef<'request' | 'upstream'>('request')
const selectedRequest = shallowRef<MonitorRelatedRequest | null>(null), errorRequest = shallowRef<MonitorRelatedRequest | null>(null)
const action = shallowRef(''), reason = shallowRef(''), duration = shallowRef('60')
const tabs = ['evidence', 'requests', 'timeline', 'history', 'deliveries'] as const
const tab = shallowRef<(typeof tabs)[number]>('evidence')
const item = computed(() => detail.value?.incident), active = computed(() => !!item.value && incidentActive(item.value))
const durations = computed(() => [15, 60, 360, 1440].map(value => ({ value: String(value), label: t('admin.ops.enhanced.minutes', { n: value }) })))
const traceOptions = computed(() => traces.value.map(row => ({ value: row.trace_id, label: `${incidentTime(row.started_at)} · #${row.account_id} · ${row.state}` })))
const sampleRequest = computed<MonitorRelatedRequest | null>(() => item.value?.sample?.request_id ? {
  request_id: item.value.sample.request_id, account_id: item.value.sample.account_id || 0, account_name: item.value.sample_account_name || '', group_id: item.value.group_id || 0, group_name: item.value.group_name || '',
  started_at: item.value.sample.request_started_at, ended_at: item.value.sample.request_ended_at, outcome: item.value.sample.request_outcome || 'unknown', client_status: 0
} : null)
const timelineRequest = computed(() => selectedRequest.value || sampleRequest.value)
const taskID = computed(() => item.value?.scope?.match(/^task:(.*)\/user:\d+\/key:\d+$/)?.[1] || '')
const orderID = computed(() => taskID.value.match(/^payment:(\d+)$/)?.[1])
const errorStart = computed(() => {
  const at = Date.parse(errorRequest.value?.started_at || '')
  return new Date((at > 0 ? at : Date.parse(item.value?.first_seen || new Date().toISOString())) - 60000).toISOString()
})
const outcome = computed(() => {
  const sampled = item.value?.sample?.request_outcome
  if (sampled && ['success', 'failed', 'cancelled'].includes(sampled)) return sampled
  const counts = detail.value?.outcomes
  return item.value?.request_count === 1 && counts ? (['success', 'failed', 'cancelled'] as const).find(key => counts[key] === 1) || 'unknown' : 'unknown'
})
const timeFields = computed(() => item.value ? {
  firstFault: item.value.first_seen, lastFault: item.value.last_fault, lastObservation: item.value.last_seen,
  resolvedAt: item.value.resolved_at, lastControl: item.value.last_control_at
} : {})
let request: AbortController | null = null, traceRequest: AbortController | null = null
async function load() {
  request?.abort(); if (!props.id) return false
  const controller = new AbortController(); request = controller; loading.value = true; error.value = ''
  try { const data = await opsMonitorAPI.detail(props.id, controller.signal); if (!controller.signal.aborted) { detail.value = data; stale.value = false; return true } }
  catch (e: any) { if (!controller.signal.aborted) error.value = e?.message || t('admin.ops.enhanced.loadError') }
  finally { if (request === controller) loading.value = false }
  return false
}
async function control(name: string) {
  if (!item.value || busy.value || stale.value) return
  const id = props.id
  busy.value = true; error.value = ''
  try {
    await opsMonitorAPI.control(item.value, name, reason.value, name === 'silence' ? new Date(Date.now() + Number(duration.value) * 60000).toISOString() : undefined)
    if (id !== props.id) return
    action.value = ''; reason.value = ''; await load(); emit('changed')
  } catch (e: any) {
    if (id !== props.id) return
    if ((e?.response?.status ?? e?.status) === 409) { stale.value = true; const refreshed = await load(); error.value = t(refreshed ? 'admin.ops.enhanced.conflictRefreshed' : 'admin.ops.enhanced.conflictRefreshFailed'); emit('changed') }
    else error.value = e?.message || t('admin.ops.enhanced.saveError')
  } finally { busy.value = false }
}
function openErrors(record: MonitorRelatedRequest, kind: 'request' | 'upstream') {
  errorRequest.value = record; errorType.value = kind; showErrors.value = true
}
function openTimeline(record: MonitorRelatedRequest) {
  selectedRequest.value = record; trace.value = null; traces.value = []; traceState.value = ''; tab.value = 'timeline'; void loadTimeline()
}
async function loadTimeline() {
  traceRequest?.abort(); trace.value = null
  const record = timelineRequest.value
  if (!record) { traceState.value = 'unlinked'; return }
  const controller = new AbortController(); traceRequest = controller; traceState.value = 'loading'
  const started = Date.parse(record.started_at || ''), ended = Date.parse(record.ended_at || '')
  const base = started > 0 ? started : ended > 0 ? ended : Date.parse(item.value!.first_seen)
  try {
    const page = await listConnectionTraces({ request_id: record.request_id, start_time: new Date(base - (started > 0 ? 60000 : 86400000)).toISOString(), end_time: new Date(Math.max(base + 60000, ended > 0 ? ended : Date.parse(item.value!.last_seen))).toISOString(), limit: 100 }, controller.signal)
    if (controller.signal.aborted) return
    traces.value = page.items
    const row = page.items.find(row => row.trace_id === traceID.value) || page.items[0]
    if (!row) { traceState.value = Date.now() - base > page.retention_days * 86400000 ? 'expired' : 'empty'; return }
    traceID.value = row.trace_id
    await loadTrace(row, controller)
  } catch { if (!controller.signal.aborted) traceState.value = 'error' }
}
async function loadTrace(row: ConnectionTrace, controller: AbortController) {
  traceState.value = 'loading'
  try {
    const result = row.detail_available ? await getConnectionTrace(row, controller.signal) : row
    if (!controller.signal.aborted) { trace.value = result; traceState.value = 'ready' }
  } catch { if (!controller.signal.aborted) traceState.value = 'error' }
}
function selectTrace(id: string | number | boolean | null) {
  const row = traces.value.find(row => row.trace_id === id)
  if (!row) return
  traceID.value = row.trace_id; traceRequest?.abort(); const controller = new AbortController(); traceRequest = controller; void loadTrace(row, controller)
}
watch(() => props.id, () => { request?.abort(); traceRequest?.abort(); stale.value = false; detail.value = null; trace.value = null; traces.value = []; selectedRequest.value = null; traceState.value = ''; tab.value = 'evidence'; action.value = ''; reason.value = ''; showErrors.value = false; errorDetail.value = false; void load() }, { immediate: true })
watch(tab, value => { if (value === 'timeline' && !trace.value && traceState.value !== 'loading') void loadTimeline() })
watch(() => props.refreshToken, () => { if (props.id && visibility.value === 'visible' && !busy.value && !loading.value) { void load(); if (tab.value === 'timeline') void loadTimeline() } })
onBeforeUnmount(() => { request?.abort(); traceRequest?.abort() })
</script>

<template>
  <BaseDialog :show="!!id" :title="t('admin.ops.enhanced.detail')" width="extra-wide" content-class="sm:!ml-auto sm:!mr-0 sm:!h-full sm:!max-h-screen sm:!rounded-r-none" @close="emit('close')">
    <p v-if="error" class="mb-3 text-sm text-amber-600" role="alert">{{ error }}</p>
    <p v-if="loading && !item" class="py-10 text-center text-sm text-gray-400">{{ t('common.loading') }}</p>
    <template v-if="item && detail">
      <div class="mb-4 flex items-start justify-between gap-3"><div><h3 class="text-base font-semibold">{{ te(`admin.ops.enhanced.rules.${item.rule_id}`) ? t(`admin.ops.enhanced.rules.${item.rule_id}`) : item.explanation.conclusion }}</h3><div class="mt-1 flex gap-3 text-xs text-gray-400"><span>{{ item.severity }} · {{ item.rule_id }}</span><OpsIncidentStatus :item="item" /><span class="font-mono">{{ item.id.slice(0, 8) }}</span></div></div><button class="btn btn-ghost btn-sm" :disabled="loading || busy" :aria-label="t('admin.ops.enhanced.refresh')" @click="load()"><Icon name="refresh" size="sm" /></button></div>
      <dl class="mb-4 grid grid-cols-2 gap-3 text-xs sm:grid-cols-3">
        <div><dt class="text-gray-400">{{ t('admin.ops.enhanced.model') }}</dt><dd class="mt-1 break-all font-medium">{{ item.model || '—' }}</dd></div>
        <div><dt class="text-gray-400">{{ t('admin.ops.enhanced.group') }}</dt><dd class="mt-1"><RouterLink v-if="item.group_id" :to="{ path: '/admin/groups', query: { group_id: item.group_id } }" class="text-primary-600">{{ item.group_name || t('admin.ops.enhanced.missingObject') }} <span class="text-gray-400">#{{ item.group_id }}</span></RouterLink><span v-else>—</span></dd></div>
        <div><dt class="text-gray-400">{{ t('admin.ops.enhanced.sampleAccount') }}</dt><dd class="mt-1"><RouterLink v-if="item.sample?.account_id" :to="{ path: '/admin/accounts', query: { account_id: item.sample.account_id } }" class="text-primary-600">{{ item.sample_account_name || t('admin.ops.enhanced.missingObject') }} <span class="text-gray-400">#{{ item.sample.account_id }}</span></RouterLink><span v-else>—</span></dd></div>
      </dl>
      <div class="mb-4 rounded-lg bg-gray-50 px-3 py-2 text-xs dark:bg-dark-800"><button class="font-medium" @click="tab = 'requests'">{{ t('admin.ops.enhanced.cumulativeRequests', { n: item.request_count }) }}</button><div class="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-gray-500"><span v-for="(count, result) in detail.outcomes" :key="result">{{ t(`admin.ops.enhanced.outcomes.${result}`) }} {{ count }}</span></div><p class="mt-1 text-gray-400">{{ t('admin.ops.enhanced.cumulativePeriod', { start: incidentTime(item.first_seen), end: incidentTime(item.last_seen) }) }}</p></div>
      <p v-if="singleRequestWait(item) && item.status === 'resolved'" class="mb-3 text-xs text-gray-500">{{ t('admin.ops.enhanced.waitResult', { result: t(`admin.ops.enhanced.outcomes.${outcome}`) }) }}</p>
      <p v-else-if="item.status === 'resolved' || item.status === 'recovering'" class="mb-3 text-xs text-gray-500">{{ t('admin.ops.enhanced.recoveryEvidence', { n: item.healthy_samples || 0, since: incidentTime(item.recovering_since) }) }}</p>
      <div class="mb-4 flex flex-wrap gap-2">
        <button v-if="active" class="btn btn-secondary btn-sm" :disabled="busy || stale" @click="control(item.acknowledged ? 'unack' : 'ack')">{{ t(item.acknowledged ? 'admin.ops.enhanced.unack' : 'admin.ops.enhanced.ack') }}</button>
        <button v-if="active && incidentSilenced(item)" class="btn btn-secondary btn-sm" :disabled="busy || stale" @click="control('unsilence')">{{ t('admin.ops.enhanced.unsilence') }}</button>
        <button v-else-if="active" class="btn btn-secondary btn-sm" @click="action = 'silence'">{{ t('admin.ops.enhanced.silence') }}</button>
        <button v-if="active" class="btn btn-ghost btn-sm" @click="action = 'close'">{{ t('admin.ops.enhanced.close') }}</button>
        <template v-if="sampleRequest"><button class="btn btn-ghost btn-sm" @click="openErrors(sampleRequest, 'request')">{{ t('admin.ops.enhanced.sampleRequestErrors') }}</button><button class="btn btn-ghost btn-sm" @click="openErrors(sampleRequest, 'upstream')">{{ t('admin.ops.enhanced.sampleUpstreamAttempts') }}</button></template>
      </div>
      <div v-if="action" class="mb-4 flex flex-wrap gap-2 rounded-xl bg-gray-50 p-3 dark:bg-dark-800"><Select v-if="action === 'silence'" v-model="duration" :options="durations" class="w-32" /><input v-model="reason" class="input min-w-40 flex-1" maxlength="300" :placeholder="t('admin.ops.enhanced.reason')" /><button class="btn btn-primary btn-sm" :disabled="busy || !reason.trim()" @click="control(action)">{{ t('common.confirm') }}</button><button class="btn btn-ghost btn-sm" @click="action = ''">{{ t('common.cancel') }}</button></div>
      <div class="mb-4 flex gap-4 overflow-x-auto border-b border-gray-100 dark:border-dark-700"><button v-for="name in tabs" :key="name" class="whitespace-nowrap border-b-2 px-0 py-2 text-sm" :class="tab === name ? 'border-primary-500 text-primary-600' : 'border-transparent text-gray-400'" @click="tab = name">{{ t(`admin.ops.enhanced.tabs.${name}`) }}</button></div>
      <div v-if="tab === 'evidence'" class="space-y-4">
        <div v-if="taskID" class="flex flex-wrap items-center gap-2 text-xs"><span class="text-gray-400">{{ t('admin.ops.enhanced.sourceRecord') }}</span><RouterLink v-if="orderID" :to="{ path: '/admin/orders', query: { order_id: orderID } }" class="text-primary-600">{{ t('admin.ops.enhanced.viewOrder') }} #{{ orderID }}</RouterLink><code v-else class="break-all">{{ taskID }}</code></div>
        <div v-for="section in ['facts', 'inferences', 'missing_evidence', 'advice'] as const" :key="section"><template v-if="item.explanation[section]?.length"><h4 class="mb-1.5 text-xs font-medium text-gray-400">{{ t(`admin.ops.enhanced.evidence.${section}`) }}</h4><ul class="space-y-1 text-sm"><li v-for="(line, index) in item.explanation[section]" :key="index" class="break-words">{{ line }}</li></ul></template></div>
        <dl class="grid grid-cols-2 gap-3 border-t border-gray-100 pt-3 text-xs dark:border-dark-700"><div v-for="(value, key) in timeFields" :key="key"><dt class="text-gray-400">{{ t(`admin.ops.enhanced.times.${key}`) }}</dt><dd class="mt-1">{{ incidentTime(value) }}</dd></div></dl>
        <p v-if="item.closed_by" class="text-xs text-gray-400">{{ t(`admin.ops.enhanced.closedBy.${item.closed_by}`) }}<template v-if="item.control_reason"> · {{ item.control_reason }}</template></p>
      </div>
      <OpsIncidentRequests v-else-if="tab === 'requests'" :incident="item" :refresh-token="refreshToken" @errors="openErrors" @timeline="openTimeline" />
      <div v-else-if="tab === 'timeline'">
        <div class="mb-3 flex items-center justify-between gap-2"><code class="break-all text-xs text-gray-400">{{ timelineRequest?.request_id || '—' }}</code><button class="btn btn-ghost btn-sm" :disabled="traceState === 'loading'" @click="loadTimeline()">{{ t('admin.ops.enhanced.refresh') }}</button></div>
        <Select v-if="traces.length > 1" :model-value="traceID" :options="traceOptions" class="mb-3" @update:model-value="selectTrace" />
        <ConnectionTimeline v-if="traceState === 'ready' && trace" :row="trace" />
        <div v-else class="py-8 text-center text-sm text-gray-400" :role="traceState === 'error' ? 'alert' : 'status'">{{ t(traceState === 'loading' ? 'common.loading' : `admin.ops.enhanced.timelineStates.${traceState || 'unlinked'}`) }}<button v-if="traceState === 'error'" class="ml-2 text-primary-600" @click="loadTimeline()">{{ t('admin.ops.enhanced.retry') }}</button></div>
      </div>
      <OpsIncidentHistory v-else-if="tab === 'history'" :detail="detail" />
      <div v-else><p class="mb-2 text-xs text-gray-500">{{ t(`admin.ops.enhanced.notificationConstraints.${detail.notification_constraint || 'unknown'}`) }}</p><p class="mb-2 text-xs text-gray-400">{{ t('admin.ops.enhanced.deliveryEvidenceHint') }}</p><OpsDeliveryTable :items="detail.deliveries || []" /></div>
    </template>
  </BaseDialog>
  <OpsErrorDetailsModal :show="showErrors" time-range="custom" :custom-start-time="errorStart" :custom-end-time="new Date().toISOString()" :request-id="errorRequest?.request_id" :error-type="errorType" initial-view="all" @update:show="showErrors = $event" @open-error-detail="errorID = $event; errorDetail = true" />
  <OpsErrorDetailModal v-model:show="errorDetail" :error-id="errorID" :error-type="errorType" />
</template>
