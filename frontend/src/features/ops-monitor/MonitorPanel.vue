<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { monitorAPI, monitorError, type MonitorLive, type MonitorSummary } from '@/api/admin/monitor'
import MonitorPulse from './MonitorPulse.vue'
import MonitorIncidentList from './MonitorIncidentList.vue'
import MonitorLiveRail from './MonitorLiveRail.vue'
import MonitorIncidentDrawer from './MonitorIncidentDrawer.vue'
import MonitorDeliveries from './MonitorDeliveries.vue'
import MonitorSources from './MonitorSources.vue'
import MonitorConfigForm from './MonitorConfigForm.vue'
import MonitorChannelsForm from './MonitorChannelsForm.vue'
import { inWindow, isOpen, recentWindows, sourceName, topLevel, type RecentWindow } from './monitorView'
const props = defineProps<{ initialIncidentId?: string }>()
const REFRESH_MS = 10000
const data = ref<MonitorSummary | null>(null); const live = ref<MonitorLive | null>(null)
const error = ref(''); const liveError = ref(''); const loading = ref(false)
const view = ref<'overview' | 'deliveries' | 'sources' | 'settings'>('overview')
const selected = ref<string | null>(props.initialIncidentId || null)
const now = ref(Date.now()); const updatedAt = ref(0); const tick = ref(0)
const abort = new AbortController()
const WINDOW_KEY = 'ops-monitor-window'
const savedWindow = Number(localStorage.getItem(WINDOW_KEY))
const windowMin = ref<RecentWindow>((recentWindows as readonly number[]).includes(savedWindow) ? (savedWindow as RecentWindow) : 60)
watch(windowMin, v => localStorage.setItem(WINDOW_KEY, String(v)))

async function refresh() {
  if (loading.value) return
  loading.value = true
  // Settings forms keep their own draft; the summary would not change under them anyway.
  const [s, l] = await Promise.allSettled([view.value === 'settings' && data.value ? Promise.resolve(data.value) : monitorAPI.summary(abort.signal), monitorAPI.live(abort.signal)])
  if (abort.signal.aborted) return
  if (s.status === 'fulfilled') { data.value = s.value; error.value = '' } else error.value = monitorError(s.reason)
  if (l.status === 'fulfilled') { live.value = l.value; liveError.value = '' } else liveError.value = monitorError(l.reason)
  if (s.status === 'fulfilled') { updatedAt.value = Date.now(); tick.value++ }
  loading.value = false
}
const staleIds = computed(() => (data.value?.sources || []).filter(s => s.source_id !== 'notification' && (['stale', 'unsupported'].includes(s.quality) || (!s.source_id.includes('reconcile') && now.value - new Date(s.last_success).getTime() > 120000))).map(s => s.source_id))
const failedSends = computed(() => (data.value?.deliveries || []).filter(d => d.state === 'dead').length)
// A rule switched off in settings is not reported on, including incidents it opened before.
const shown = computed(() => (data.value?.incidents || []).filter(i => data.value?.config.rules[i.rule_id]?.enabled !== false))
const openCount = computed(() => topLevel(shown.value).filter(i => isOpen(i) && inWindow(i, now.value, windowMin.value)).length)
const freshness = computed(() => {
  if (!updatedAt.value) return '正在连接'
  const s = Math.round((now.value - updatedAt.value) / 1000)
  return s < 3 ? '刚刚更新' : `${s} 秒前更新`
})
const lagging = computed(() => updatedAt.value > 0 && now.value - updatedAt.value > REFRESH_MS * 3)
const tabs = computed(() => [
  { id: 'overview', title: '总览', badge: openCount.value || 0 },
  { id: 'deliveries', title: '通知记录', badge: failedSends.value || 0 },
  { id: 'sources', title: '数据源', badge: staleIds.value.length || 0 },
  { id: 'settings', title: '设置', badge: 0 },
] as const)

let poll: ReturnType<typeof setInterval> | undefined; let clockTimer: ReturnType<typeof setInterval> | undefined
function onVisible() { if (document.visibilityState === 'visible') refresh() }
onMounted(() => {
  refresh()
  poll = setInterval(() => { if (document.visibilityState !== 'hidden') refresh() }, REFRESH_MS)
  clockTimer = setInterval(() => { now.value = Date.now() }, 1000)
  document.addEventListener('visibilitychange', onVisible)
})
onUnmounted(() => { clearInterval(poll); clearInterval(clockTimer); document.removeEventListener('visibilitychange', onVisible); abort.abort() })
watch(() => props.initialIncidentId, id => { if (id) selected.value = id })
watch(view, (v, old) => { if (old === 'settings' && v !== 'settings') refresh() })
</script>
<template>
  <div class="mx-auto max-w-[1400px] px-4 pb-16 pt-5 sm:px-6 lg:px-8">
    <header class="mb-5 flex flex-wrap items-center gap-x-6 gap-y-3">
      <h1 class="text-lg font-semibold text-gray-900 dark:text-white">异常监控</h1>
      <nav class="order-3 -mx-1 flex w-full gap-1 overflow-x-auto sm:order-none sm:w-auto" aria-label="页面">
        <button v-for="t in tabs" :key="t.id" type="button" class="flex shrink-0 items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm transition-colors" :class="view === t.id ? 'bg-gray-900 text-white dark:bg-white dark:text-gray-900' : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-800'" :aria-current="view === t.id ? 'page' : undefined" @click="view = t.id">
          {{ t.title }}<span v-if="t.badge" class="min-w-[1.25rem] rounded-full px-1 text-center text-[11px] tabular-nums" :class="view === t.id ? 'bg-white/20' : t.id === 'overview' ? 'bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-300' : 'bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300'">{{ t.badge }}</span>
        </button>
      </nav>
      <div class="ml-auto flex items-center gap-3 text-xs text-gray-500">
        <span v-if="data" class="rounded-md px-2 py-1" :class="data.config.shadow ? 'bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300' : 'bg-gray-100 dark:bg-dark-800'">{{ data.config.shadow ? '影子模式，不发通知' : '通知已开启' }}</span>
        <span class="inline-flex items-center gap-1.5" role="status" aria-live="polite">
          <span class="relative flex h-2 w-2">
            <span v-if="!lagging && !error" class="absolute inline-flex h-full w-full animate-ping rounded-full bg-primary-400 opacity-60 motion-reduce:hidden" />
            <span class="relative inline-flex h-2 w-2 rounded-full" :class="error || lagging ? 'bg-amber-500' : 'bg-primary-500'" />
          </span>
          <span class="tabular-nums">{{ error ? '连接中断，正在重试' : `实时，${freshness}` }}</span>
        </span>
      </div>
    </header>

    <p v-if="error" role="alert" class="mb-5 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-950/50 dark:text-amber-200">监控服务暂时连不上：{{ error }}。下面显示的是上次读取的内容，页面会自动重试；网关转发不受影响。</p>
    <p v-if="!data && !error" class="py-24 text-center text-sm text-gray-500">正在读取监控状态…</p>

    <template v-if="data">
      <template v-if="view === 'overview'">
        <MonitorPulse v-model:window-min="windowMin" :live="live" :incidents="shown" :rules="data.rules" :now="now" :stale="staleIds.map(sourceName)" :live-error="liveError" />
        <div class="mt-7 grid gap-8 lg:grid-cols-[minmax(0,1fr)_340px]">
          <MonitorIncidentList :window-min="windowMin" :incidents="shown" :rules="data.rules" :names="data.names" :selected="selected" :now="now" @open="selected = $event" />
          <MonitorLiveRail :live="live" :summary="data" :stale="staleIds" @view="view = $event as typeof view" />
        </div>
      </template>
      <MonitorDeliveries v-else-if="view === 'deliveries'" :summary="data" @open="selected = $event" />
      <MonitorSources v-else-if="view === 'sources'" :summary="data" :stale="staleIds" :now="now" />
      <div v-else class="mx-auto max-w-5xl">
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">通知通道</h2>
        <MonitorChannelsForm @saved="refresh" />
        <h2 class="mt-10 text-base font-semibold text-gray-900 dark:text-white">通知与检查规则</h2>
        <MonitorConfigForm :config="data.config" :rules="data.rules" :transports="data.transports || {}" @saved="refresh" />
      </div>
    </template>
    <MonitorIncidentDrawer :id="selected" :rules="data?.rules || []" :names="data?.names" :tick="tick" @close="selected = null" @changed="refresh" @open="selected = $event" />
  </div>
</template>
