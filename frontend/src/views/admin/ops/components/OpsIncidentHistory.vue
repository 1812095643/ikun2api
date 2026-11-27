<script setup lang="ts">
import { ref, shallowRef, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { opsMonitorAPI, type MonitorDetail, type MonitorHistory } from '@/api/admin/opsMonitor'
import { incidentTime } from '../utils/incidentPresentation'

const props = defineProps<{ detail: MonitorDetail }>()
const { t } = useI18n()
const kind = shallowRef('operations'), items = ref<MonitorHistory[]>([]), more = shallowRef(false), before = shallowRef(0), loading = shallowRef(false), error = shallowRef('')
let request: AbortController | null = null
async function load(append = false) {
  request?.abort(); const controller = new AbortController(); request = controller; loading.value = true; error.value = ''
  try {
    const data = await opsMonitorAPI.history(props.detail.incident.id, append ? before.value : 0, kind.value, controller.signal)
    if (!controller.signal.aborted) { items.value = append ? [...items.value, ...data.items] : data.items; more.value = data.has_more; before.value = data.before_id }
  } catch { if (!controller.signal.aborted) error.value = t('admin.ops.enhanced.loadError') }
  finally { if (request === controller) loading.value = false }
}
watch(() => props.detail.incident.id, () => { items.value = props.detail.history; more.value = props.detail.history_has_more; before.value = props.detail.history_before_id }, { immediate: true })
watch(kind, () => void load())
watch(() => props.detail.incident.revision, () => { if (items.value.length <= 30) void load() })
onBeforeUnmount(() => request?.abort())
</script>

<template>
  <div>
    <div class="mb-3 flex gap-3 text-xs"><button v-for="value in ['operations', 'observations']" :key="value" :class="kind === value ? 'text-primary-600' : 'text-gray-400'" @click="kind = value">{{ t(`admin.ops.enhanced.historyKinds.${value}`) }}</button></div>
    <p v-if="error" class="mb-2 text-xs text-amber-600">{{ error }} <button @click="load()">{{ t('admin.ops.enhanced.retry') }}</button></p>
    <div v-for="record in items" :key="record.transition_id" class="mb-3 border-b border-gray-100 pb-3 text-xs dark:border-dark-700">
      <div class="flex justify-between gap-3"><span class="font-medium">{{ t(`admin.ops.enhanced.historyActions.${record.action || 'legacy'}`) }}<template v-if="record.actor"> · {{ record.actor_name || '#' + record.actor }}</template></span><time class="whitespace-nowrap text-gray-400">{{ incidentTime(record.recorded_at) }}</time></div>
      <div class="mt-1 text-gray-500"><template v-if="record.previous_status">{{ t(`admin.ops.enhanced.status.${record.previous_status}`) }} → </template>{{ t(`admin.ops.enhanced.status.${record.status}`) }}<template v-if="record.action === 'silence'"> · {{ t('admin.ops.enhanced.silencedUntil') }} {{ incidentTime(record.silenced_until) }}</template></div>
      <p v-if="record.control_reason && !['observation', 'legacy'].includes(record.action)" class="mt-1 break-words text-gray-400">{{ record.control_reason }}</p>
    </div>
    <p v-if="!items.length && !loading && !error" class="py-6 text-center text-xs text-gray-400">{{ t('admin.ops.enhanced.noHistory') }}</p>
    <button v-if="more" class="btn btn-ghost btn-sm" :disabled="loading" @click="load(true)">{{ t('admin.ops.enhanced.loadMore') }}</button>
  </div>
</template>
