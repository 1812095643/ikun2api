<script setup lang="ts">
import { computed, onBeforeUnmount, shallowRef, ref, watch } from 'vue'
import { useDocumentVisibility } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import DataTable from '@/components/common/DataTable.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { opsMonitorAPI, type IncidentQuery, type MonitorHealth, type MonitorIncident, type MonitorRule } from '@/api/admin/opsMonitor'
import OpsIncidentDetail from './OpsIncidentDetail.vue'
import OpsAlertEventsCard from './OpsAlertEventsCard.vue'
import OpsIncidentStatus from './OpsIncidentStatus.vue'
import OpsIncidentHealth from './OpsIncidentHealth.vue'
import { incidentTime } from '../utils/incidentPresentation'

const props = defineProps<{ filters: IncidentQuery; refreshToken?: number }>()
const emit = defineEmits<{ settings: [] }>()
const { t, te } = useI18n(), route = useRoute(), router = useRouter()
const items = ref<MonitorIncident[]>([]), health = shallowRef<MonitorHealth | null>(null), rules = ref<MonitorRule[]>([])
const loading = shallowRef(false), error = shallowRef(''), status = shallowRef('active'), severity = shallowRef(''), rule = shallowRef('')
const healthError = shallowRef(''), updatesPending = shallowRef(false)
const model = shallowRef('')
const legacy = shallowRef(false), selectedID = shallowRef(''), hasMore = shallowRef(false), next = shallowRef<{ before_time: number; before_id: string } | null>(null)
const cursors = ref<({ before_time: number; before_id: string } | undefined)[]>([undefined]), page = shallowRef(0)
const visibility = useDocumentVisibility()
let request: AbortController | null = null, sequence = 0
const columns = computed(() => ['severity', 'title', 'scope', 'impact', 'status', 'last_fault'].map(key => ({ key, label: t(`admin.ops.enhanced.columns.${key}`) })))
const statusOptions = computed(() => ['', 'active', 'acknowledged', 'silenced', 'recovering', 'resolved', 'closed'].map(value => ({ value, label: value ? t(`admin.ops.enhanced.status.${value}`) : t('common.all') })))
const severityOptions = computed(() => [{ value: '', label: t('common.all') }, ...['P0', 'P1', 'P2', 'P3'].map(value => ({ value, label: value }))])
const ruleOptions = computed(() => [{ value: '', label: t('common.all') }, ...rules.value.map(r => ({ value: r.id, label: `${r.id} · ${te(`admin.ops.enhanced.rules.${r.id}`) ? t(`admin.ops.enhanced.rules.${r.id}`) : r.title}` }))])
async function load(reset = false) {
  if (legacy.value) return
  if (reset) { page.value = 0; cursors.value = [undefined]; updatesPending.value = false }
  request?.abort(); const current = ++sequence; request = new AbortController(); loading.value = true; error.value = ''
  try {
    const params = { ...props.filters, model: model.value.trim() || props.filters.model, status: status.value || undefined, severity: severity.value || undefined, rule: rule.value || undefined, ...cursors.value[page.value] }
    const signal = request.signal
    await Promise.allSettled([
      opsMonitorAPI.incidents(params, signal).then(result => {
        if (current !== sequence) return
        items.value = result.items; hasMore.value = result.has_more; next.value = { before_time: result.before_time, before_id: result.before_id }
      }).catch(e => { if (current === sequence && !signal.aborted) error.value = e?.message || t('admin.ops.enhanced.loadError') })
        .finally(() => { if (current === sequence) loading.value = false }),
      opsMonitorAPI.health(signal).then(snapshot => { if (current === sequence) { health.value = snapshot; healthError.value = '' } })
        .catch(() => { if (current === sequence && !signal.aborted) healthError.value = t('admin.ops.enhanced.healthUnavailable') })
    ])
  } catch (e: any) { if (current === sequence && e?.code !== 'ERR_CANCELED') error.value = e?.message || t('admin.ops.enhanced.loadError') }
  finally { if (current === sequence) loading.value = false }
}
function changePage(direction: number) { if (direction > 0 && next.value) cursors.value[page.value + 1] = next.value; page.value += direction; void load() }
function open(item: MonitorIncident) { selectedID.value = item.id; void router.replace({ query: { ...route.query, incident: item.id } }) }
function close() { selectedID.value = ''; const query = { ...route.query }; delete query.incident; void router.replace({ query }) }
watch(() => props.filters, () => void load(true), { deep: true, immediate: true })
watch(() => props.refreshToken, () => {
  if (visibility.value !== 'visible') return
  if (page.value > 0) updatesPending.value = true
  else if (!loading.value) void load()
})
watch([model, status, severity, rule, legacy], () => void load(true))
watch(() => route.query.incident, id => { selectedID.value = typeof id === 'string' ? id : '' }, { immediate: true })
void opsMonitorAPI.config().then(data => { rules.value = data.rules }).catch(() => undefined)
onBeforeUnmount(() => { ++sequence; request?.abort() })
</script>

<template>
  <section class="card overflow-hidden">
    <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 px-4 py-3 dark:border-dark-700">
      <div class="flex flex-wrap items-center gap-3"><h3 class="text-sm font-semibold">{{ t('admin.ops.enhanced.title') }}</h3><OpsIncidentHealth :health="health" :error="healthError" /></div>
      <div class="flex items-center gap-2"><button class="btn btn-ghost btn-sm" @click="legacy = !legacy">{{ t(legacy ? 'admin.ops.enhanced.title' : 'admin.ops.enhanced.legacy') }}</button><button class="btn btn-ghost btn-sm" @click="emit('settings')"><Icon name="cog" size="sm" /></button><button class="btn btn-ghost btn-sm" :disabled="loading" @click="load()"><Icon name="refresh" size="sm" /></button></div>
    </div>
    <template v-if="!legacy">
      <div class="grid grid-cols-2 gap-2 px-4 py-3 sm:grid-cols-[140px_100px_minmax(160px,1fr)_minmax(140px,1fr)]"><Select v-model="status" :options="statusOptions" /><Select v-model="severity" :options="severityOptions" /><Select v-model="rule" :options="ruleOptions" class="col-span-2 sm:col-span-1" searchable /><input v-model.lazy="model" class="input" :placeholder="t('admin.ops.enhanced.model')" maxlength="256" /></div>
      <p v-if="error" class="px-4 py-2 text-sm text-red-600" role="alert">{{ error }}</p>
      <div class="flex flex-wrap items-center justify-between gap-2 px-4 pb-2 text-xs text-gray-400"><span>{{ t(['active', 'acknowledged', 'silenced', 'recovering'].includes(status) ? 'admin.ops.enhanced.activeScopeHint' : 'admin.ops.enhanced.historyScopeHint') }}</span><button v-if="updatesPending" class="text-primary-600" @click="load(true)">{{ t('admin.ops.enhanced.returnLatest') }}</button></div>
      <DataTable :columns="columns" :data="items" :loading="loading" row-key="id" clickable-rows @row-click="open">
        <template #cell-severity="{ row }"><span class="rounded-md px-2 py-0.5 text-xs font-semibold" :class="row.severity === 'P0' ? 'bg-red-50 text-red-600 dark:bg-red-950/40' : row.severity === 'P1' ? 'bg-amber-50 text-amber-700 dark:bg-amber-950/40' : 'bg-gray-100 text-gray-600 dark:bg-dark-700'">{{ row.severity }}</span></template>
        <template #cell-title="{ row }"><div class="max-w-sm truncate text-sm font-medium">{{ te(`admin.ops.enhanced.rules.${row.rule_id}`) ? t(`admin.ops.enhanced.rules.${row.rule_id}`) : row.explanation.conclusion }}</div><span class="text-xs text-gray-400">{{ row.rule_id }} · {{ row.id.slice(0, 8) }}</span></template>
        <template #cell-scope="{ row }"><div class="max-w-52 truncate text-xs">{{ row.model || '—' }}</div><span class="text-xs text-gray-400">{{ row.platform || '—' }}</span><div v-if="row.group_id" class="text-xs"><RouterLink :to="{ path: '/admin/groups', query: { group_id: row.group_id } }" class="text-primary-600" @click.stop>{{ row.group_name || t('admin.ops.enhanced.missingObject') }}</RouterLink><span class="ml-1 text-gray-400">#{{ row.group_id }}</span></div><RouterLink v-if="row.sample?.account_id" :to="{ path: '/admin/accounts', query: { account_id: row.sample.account_id } }" class="text-xs text-primary-600" @click.stop>{{ row.sample_account_name || t('admin.ops.enhanced.missingObject') }} <span class="text-gray-400">#{{ row.sample.account_id }} · {{ t('admin.ops.enhanced.sampleAccount') }}</span></RouterLink></template>
        <template #cell-impact="{ row }"><span class="whitespace-nowrap text-xs tabular-nums">{{ t('admin.ops.enhanced.impact', { requests: row.request_count, accounts: row.account_count, users: row.user_count }) }}</span></template>
        <template #cell-status="{ row }"><OpsIncidentStatus :item="row" /></template>
        <template #cell-last_fault="{ row }"><span class="whitespace-nowrap text-xs tabular-nums">{{ incidentTime(row.last_fault || row.first_seen) }}</span></template>
      </DataTable>
      <div class="flex items-center justify-end gap-3 px-4 py-2"><span class="text-xs text-gray-400">{{ page + 1 }}</span><button class="btn btn-ghost btn-sm" :disabled="page === 0 || loading" @click="changePage(-1)">{{ t('admin.ops.enhanced.previous') }}</button><button class="btn btn-ghost btn-sm" :disabled="!hasMore || loading" @click="changePage(1)">{{ t('admin.ops.enhanced.next') }}</button></div>
    </template>
    <OpsAlertEventsCard v-else />
    <OpsIncidentDetail :id="selectedID" :refresh-token="refreshToken" @close="close" @changed="load()" />
  </section>
</template>
