<script setup lang="ts">
import { computed, onBeforeUnmount, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import ConnectionTimeline from '@/components/admin/diagnostics/ConnectionTimeline.vue'
import { listConnectionTraces, getConnectionTrace, type ConnectionTrace } from '@/api/admin/connectionDiagnostics'
import { opsMonitorAPI, type MonitorDetail, type MonitorIncident } from '@/api/admin/opsMonitor'
import OpsErrorDetailsModal from './OpsErrorDetailsModal.vue'
import OpsErrorDetailModal from './OpsErrorDetailModal.vue'
import OpsDeliveryTable from './OpsDeliveryTable.vue'

const props = defineProps<{ id: string }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const { t, te } = useI18n()
const detail = shallowRef<MonitorDetail | null>(null), loading = shallowRef(false), busy = shallowRef(false), error = shallowRef('')
const trace = shallowRef<ConnectionTrace | null>(null), traceState = shallowRef(''), showErrors = shallowRef(false), errorID = shallowRef<number | null>(null), errorDetail = shallowRef(false)
const action = shallowRef(''), reason = shallowRef(''), duration = shallowRef('60')
const tabs = ['evidence', 'timeline', 'history', 'deliveries'] as const
const tab = shallowRef<(typeof tabs)[number]>('evidence')
const item = computed(() => detail.value?.incident), active = computed(() => item.value && !['resolved', 'closed', 'superseded'].includes(item.value.status))
const durations = computed(() => [15, 60, 360, 1440].map(value => ({ value: String(value), label: t('admin.ops.enhanced.minutes', { n: value }) })))
let request: AbortController | null = null
function status(i: MonitorIncident) { if (i.silenced_until && Date.parse(i.silenced_until) > Date.now()) return 'silenced'; if (i.acknowledged && i.status === 'firing') return 'acknowledged'; return i.status === 'candidate' ? 'active' : i.status }
async function load() {
  request?.abort(); if (!props.id) return
  const controller = new AbortController(); request = controller; loading.value = true; error.value = ''
  try { const data = await opsMonitorAPI.detail(props.id, controller.signal); if (!controller.signal.aborted) detail.value = data }
  catch (e: any) { if (!controller.signal.aborted) error.value = e?.message || t('admin.ops.enhanced.loadError') }
  finally { if (request === controller) loading.value = false }
}
async function control(name: string) {
  if (!item.value || busy.value) return
  busy.value = true; error.value = ''
  try { await opsMonitorAPI.control(item.value, name, reason.value, name === 'silence' ? new Date(Date.now() + Number(duration.value) * 60000).toISOString() : undefined); action.value = ''; reason.value = ''; await load(); emit('changed') }
  catch (e: any) { error.value = e?.message || t('admin.ops.enhanced.saveError') }
  finally { busy.value = false }
}
async function loadTimeline() {
  if (!item.value?.sample?.request_id || !request || trace.value) return
  const controller = request, sample = item.value.sample
  const base = sample.request_started_at && Date.parse(sample.request_started_at) > 0 ? Date.parse(sample.request_started_at) : Date.parse(item.value.first_seen)
  traceState.value = 'loading'
  try {
    const page = await listConnectionTraces({ request_id: sample.request_id, start_time: new Date(base - 60000).toISOString(), end_time: new Date(Math.min(Date.now() + 60000, base + 86400000)).toISOString(), account_id: sample.account_id, limit: 10 }, controller.signal)
    const row = page.items[0]; if (!row) { if (!controller.signal.aborted) traceState.value = 'empty'; return }
    const result = row.detail_available ? await getConnectionTrace(row, controller.signal) : row
    if (!controller.signal.aborted) { trace.value = result; traceState.value = '' }
  } catch { if (!controller.signal.aborted) traceState.value = 'error' }
}
watch(() => props.id, () => { request?.abort(); detail.value = null; trace.value = null; traceState.value = ''; tab.value = 'evidence'; action.value = ''; reason.value = ''; showErrors.value = false; errorDetail.value = false; void load() }, { immediate: true })
watch(tab, value => { if (value === 'timeline') void loadTimeline() })
onBeforeUnmount(() => request?.abort())
</script>

<template>
  <BaseDialog :show="!!id" :title="t('admin.ops.enhanced.detail')" width="extra-wide" content-class="sm:!ml-auto sm:!mr-0 sm:!h-full sm:!max-h-screen sm:!rounded-r-none" @close="emit('close')">
    <p v-if="error" class="mb-3 text-sm text-red-600" role="alert">{{ error }}</p>
    <p v-if="loading && !item" class="py-10 text-center text-sm text-gray-400">{{ t('common.loading') }}</p>
    <template v-if="item">
      <div class="mb-4 flex flex-wrap items-start justify-between gap-3"><div><h3 class="text-base font-semibold">{{ te(`admin.ops.enhanced.rules.${item.rule_id}`) ? t(`admin.ops.enhanced.rules.${item.rule_id}`) : item.explanation.conclusion }}</h3><p class="mt-1 text-xs text-gray-400">{{ item.severity }} · {{ item.rule_id }} · {{ t(`admin.ops.enhanced.status.${status(item)}`) }}</p></div><span class="font-mono text-xs text-gray-400">{{ item.id.slice(0, 8) }}</span></div>
      <dl class="mb-4 grid grid-cols-2 gap-x-4 gap-y-2 text-xs sm:grid-cols-4"><div v-for="(value, key) in { model: item.model || '—', group: item.group_id || '—', account: item.sample?.account_id || '—', requests: item.request_count }" :key="key"><dt class="text-gray-400">{{ t(`admin.ops.enhanced.${key}`) }}</dt><dd class="mt-1 truncate font-medium">{{ value }}</dd></div></dl>
      <div class="mb-4 flex flex-wrap gap-2">
        <button v-if="active" class="btn btn-secondary btn-sm" :disabled="busy" @click="control(item.acknowledged ? 'unack' : 'ack')">{{ t(item.acknowledged ? 'admin.ops.enhanced.unack' : 'admin.ops.enhanced.ack') }}</button>
        <button v-if="active && item.silenced_until && Date.parse(item.silenced_until) > Date.now()" class="btn btn-secondary btn-sm" :disabled="busy" @click="control('unsilence')">{{ t('admin.ops.enhanced.unsilence') }}</button>
        <button v-else-if="active" class="btn btn-secondary btn-sm" @click="action = 'silence'">{{ t('admin.ops.enhanced.silence') }}</button>
        <button v-if="active" class="btn btn-ghost btn-sm" @click="action = 'close'">{{ t('admin.ops.enhanced.close') }}</button>
        <button v-if="item.sample?.request_id || item.sample?.account_id" class="btn btn-ghost btn-sm" @click="showErrors = true">{{ t('admin.ops.enhanced.errors') }}</button>
      </div>
      <div v-if="action" class="mb-4 flex flex-wrap gap-2 rounded-xl bg-gray-50 p-3 dark:bg-dark-800"><Select v-if="action === 'silence'" v-model="duration" :options="durations" class="w-32" /><input v-model="reason" class="input min-w-40 flex-1" maxlength="300" :placeholder="t('admin.ops.enhanced.reason')" /><button class="btn btn-primary btn-sm" :disabled="busy || !reason.trim()" @click="control(action)">{{ t('common.confirm') }}</button><button class="btn btn-ghost btn-sm" @click="action = ''">{{ t('common.cancel') }}</button></div>
      <div class="mb-4 flex gap-4 border-b border-gray-100 dark:border-dark-700"><button v-for="name in tabs" :key="name" class="border-b-2 px-0 py-2 text-sm" :class="tab === name ? 'border-primary-500 text-primary-600' : 'border-transparent text-gray-400'" @click="tab = name">{{ t(`admin.ops.enhanced.tabs.${name}`) }}</button></div>
      <div v-if="tab === 'evidence'" class="space-y-4">
        <div v-for="section in ['facts', 'inferences', 'missing_evidence', 'advice'] as const" :key="section"><template v-if="item.explanation[section]?.length"><h4 class="mb-1.5 text-xs font-medium text-gray-400">{{ t(`admin.ops.enhanced.evidence.${section}`) }}</h4><ul class="space-y-1 text-sm"><li v-for="(line, index) in item.explanation[section]" :key="index" class="break-words">{{ line }}</li></ul></template></div>
        <div class="border-t border-gray-100 pt-3 text-xs text-gray-400 dark:border-dark-700">{{ new Date(item.first_seen).toLocaleString() }} → {{ new Date(item.last_seen).toLocaleString() }}<p v-if="item.closed_by" class="mt-1">{{ t(`admin.ops.enhanced.closedBy.${item.closed_by}`) }}<template v-if="item.control_reason"> · {{ item.control_reason }}</template></p></div>
      </div>
      <div v-else-if="tab === 'timeline'"><ConnectionTimeline v-if="trace" :row="trace" /><p v-else class="py-8 text-center text-sm text-gray-400">{{ t(traceState === 'loading' ? 'common.loading' : 'admin.ops.enhanced.timelineUnavailable') }}</p></div>
      <div v-else-if="tab === 'history'" class="space-y-3"><div v-for="(record, index) in detail?.history" :key="`${record.revision}-${index}`" class="flex justify-between gap-4 border-b border-gray-100 pb-2 text-xs dark:border-dark-700"><span>{{ t(`admin.ops.enhanced.status.${status(record)}`) }}<template v-if="record.control_reason"> · {{ record.control_reason }}</template></span><span class="whitespace-nowrap text-gray-400">{{ new Date(record.last_seen).toLocaleString() }}</span></div></div>
      <OpsDeliveryTable v-else :items="detail?.deliveries || []" />
    </template>
  </BaseDialog>
  <OpsErrorDetailsModal :show="showErrors" time-range="custom" :custom-start-time="item ? new Date(Date.parse(item.first_seen) - 60000).toISOString() : undefined" :custom-end-time="new Date().toISOString()" :platform="item?.platform" :group-id="item?.group_id" :account-id="item?.sample?.account_id" :request-id="item?.sample?.request_id" :model="item?.model" error-type="upstream" @update:show="showErrors = $event" @open-error-detail="errorID = $event; errorDetail = true" />
  <OpsErrorDetailModal v-model:show="errorDetail" :error-id="errorID" error-type="upstream" />
</template>
