<script setup lang="ts">
import { onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { opsMonitorAPI, type MonitorIncident, type MonitorRelatedAccount, type MonitorRelatedRequest } from '@/api/admin/opsMonitor'
import { incidentTime } from '../utils/incidentPresentation'

const props = defineProps<{ incident: MonitorIncident; refreshToken?: number }>()
const emit = defineEmits<{ errors: [request: MonitorRelatedRequest, kind: 'request' | 'upstream']; timeline: [request: MonitorRelatedRequest] }>()
const { t } = useI18n()
const requests = ref<MonitorRelatedRequest[]>([]), accounts = ref<MonitorRelatedAccount[]>([])
const page = shallowRef(0), cursors = ref(['']), next = shallowRef(''), more = shallowRef(false), accountsNext = shallowRef(''), accountsMore = shallowRef(false)
const loading = shallowRef(false), accountLoading = shallowRef(false), error = shallowRef(''), accountError = shallowRef('')
let controller: AbortController | null = null, accountController: AbortController | null = null
async function load() {
  controller?.abort(); const current = new AbortController(); controller = current; loading.value = true; error.value = ''
  try {
    const data = await opsMonitorAPI.requests(props.incident.id, cursors.value[page.value], current.signal)
    if (!current.signal.aborted) { requests.value = data.items; more.value = data.has_more; next.value = data.after_id }
  } catch { if (!current.signal.aborted) error.value = t('admin.ops.enhanced.loadError') }
  finally { if (controller === current) loading.value = false }
}
async function loadAccounts(append = false) {
  accountController?.abort(); const current = new AbortController(); accountController = current; accountLoading.value = true; accountError.value = ''
  try {
    const data = await opsMonitorAPI.accounts(props.incident.id, append ? accountsNext.value : '', current.signal)
    if (!current.signal.aborted) { accounts.value = append ? [...accounts.value, ...data.items] : data.items; accountsMore.value = data.has_more; accountsNext.value = data.after_id }
  } catch { if (!current.signal.aborted) accountError.value = t('admin.ops.enhanced.loadError') }
  finally { if (accountController === current) accountLoading.value = false }
}
function changePage(direction: number) {
  if (direction > 0) cursors.value[page.value + 1] = next.value
  page.value += direction; void load()
}
watch(() => props.incident.id, () => { page.value = 0; cursors.value = ['']; accounts.value = []; void load(); void loadAccounts() }, { immediate: true })
watch(() => props.refreshToken, () => { if (page.value === 0) void load() })
onBeforeUnmount(() => { controller?.abort(); accountController?.abort() })
</script>

<template>
  <div class="space-y-4">
    <section v-if="incident.account_count">
      <h4 class="mb-2 text-xs font-medium text-gray-500">{{ t('admin.ops.enhanced.allAccounts', { n: incident.account_count }) }}</h4>
      <div class="flex flex-wrap gap-2"><template v-for="account in accounts" :key="account.id"><span v-if="account.deleted" class="rounded-md bg-gray-100 px-2 py-1 text-xs text-gray-400 dark:bg-dark-700">{{ account.name || t('admin.ops.enhanced.missingObject') }} #{{ account.id }} · {{ t('admin.ops.enhanced.deleted') }}</span><RouterLink v-else :to="{ path: '/admin/accounts', query: { account_id: account.id } }" class="rounded-md bg-gray-100 px-2 py-1 text-xs text-primary-600 dark:bg-dark-700">{{ account.name || t('admin.ops.enhanced.missingObject') }} #{{ account.id }}</RouterLink></template><button v-if="accountsMore" class="text-xs text-primary-600" :disabled="accountLoading" @click="loadAccounts(true)">{{ t('admin.ops.enhanced.loadMore') }}</button></div>
      <button v-if="accountError" class="mt-2 text-xs text-amber-600" @click="loadAccounts()">{{ accountError }} · {{ t('admin.ops.enhanced.retry') }}</button>
    </section>
    <p class="text-xs text-gray-400">{{ t('admin.ops.enhanced.cumulativeHint') }}</p>
    <p v-if="error" class="text-sm text-amber-600" role="alert">{{ error }} <button @click="load()">{{ t('admin.ops.enhanced.retry') }}</button></p>
    <div v-for="request in requests" :key="request.request_id" class="border-b border-gray-100 pb-3 text-xs dark:border-dark-700">
      <div class="flex flex-wrap justify-between gap-2"><code class="break-all text-gray-600 dark:text-gray-300">{{ request.request_id }}</code><span>{{ t(`admin.ops.enhanced.outcomes.${request.outcome}`) }}<template v-if="request.client_status"> · HTTP {{ request.client_status }}</template></span></div>
      <div class="mt-1 flex flex-wrap justify-between gap-2 text-gray-400"><span>{{ request.account_name }}<template v-if="request.account_id"> #{{ request.account_id }}</template></span><span>{{ incidentTime(request.ended_at) }}</span></div>
      <div class="mt-2 flex flex-wrap gap-3 text-primary-600"><button @click="emit('errors', request, 'request')">{{ t('admin.ops.enhanced.requestErrors') }}</button><button @click="emit('errors', request, 'upstream')">{{ t('admin.ops.enhanced.upstreamAttempts') }}</button><button @click="emit('timeline', request)">{{ t('admin.ops.enhanced.tabs.timeline') }}</button></div>
    </div>
    <p v-if="!loading && !requests.length && !error" class="py-6 text-center text-sm text-gray-400">{{ t('admin.ops.enhanced.noLinkedRequests') }}</p>
    <div class="flex items-center justify-end gap-3 text-xs"><span class="text-gray-400">{{ page + 1 }}</span><button class="btn btn-ghost btn-sm" :disabled="page === 0 || loading" @click="changePage(-1)">{{ t('admin.ops.enhanced.previous') }}</button><button class="btn btn-ghost btn-sm" :disabled="!more || loading" @click="changePage(1)">{{ t('admin.ops.enhanced.next') }}</button></div>
  </div>
</template>
