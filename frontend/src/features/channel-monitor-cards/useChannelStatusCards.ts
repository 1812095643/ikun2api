import { computed, onBeforeUnmount, onMounted, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import * as api from '@/api/channelMonitorV2'
import userGroupsAPI from '@/api/groups'
import type { MonitorFilter, MonitorMatrixResponse, MonitorRange, MonitorSnapshot } from '@/api/channelMonitorV2'
import { extractApiErrorMessage } from '@/utils/apiError'

export function useChannelStatusCards() {
  const { t } = useI18n()
  const range = shallowRef<MonitorRange>('90m')
  const snapshot = shallowRef<MonitorSnapshot | null>(null)
  const matrix = shallowRef<MonitorMatrixResponse | null>(null)
  const loading = shallowRef(false)
  const error = shallowRef('')
  const rates = shallowRef<Record<number, number>>({})
  const countdown = shallowRef(0)
  let request: AbortController | null = null
  let refreshAt = 0
  let refreshTimer: ReturnType<typeof setTimeout> | undefined
  let tickTimer: ReturnType<typeof setInterval> | undefined
  let stopped = false
  const sections = computed(() => {
    const result = new Map<string, NonNullable<MonitorMatrixResponse['items']>>()
    for (const row of matrix.value?.items ?? []) {
      if (!row.group_id || row.group_id <= 0) continue
      const list = result.get(row.platform) ?? []
      list.push(row)
      result.set(row.platform, list)
    }
    return [...result].map(([platform, rows]) => ({ platform, rows: rows.sort((a, b) => (a.group_id ?? 0) - (b.group_id ?? 0)) }))
  })
  const timelineLength = computed(() => ({ '90m': 18, '24h': 24, '7d': 14, '30d': 30 })[range.value])
  function schedule(seconds: number) {
    clearTimeout(refreshTimer)
    if (stopped) return
    refreshAt = Date.now() + Math.max(10, seconds) * 1000
    countdown.value = Math.ceil((refreshAt - Date.now()) / 1000)
    refreshTimer = setTimeout(() => { if (!document.hidden) void reload() }, Math.max(10, seconds) * 1000)
  }
  async function reload() {
    request?.abort()
    clearTimeout(refreshTimer)
    const controller = new AbortController()
    request = controller
    loading.value = true
    const filter: MonitorFilter = { range: range.value, platforms: [], groupIds: [], models: [] }
    try {
      const [nextSnapshot, nextMatrix] = await Promise.all([
        api.getSnapshot(filter, false, controller.signal),
        api.getMatrix(filter, 'platform_group', false, controller.signal),
      ])
      if (stopped || controller.signal.aborted || request !== controller) return
      snapshot.value = nextSnapshot
      matrix.value = nextMatrix
      error.value = ''
      schedule(nextSnapshot.coverage.bootstrap?.active ? 10 : nextSnapshot.config.refresh_interval_seconds)
    } catch (cause) {
      if (stopped || controller.signal.aborted || request !== controller) return
      error.value = extractApiErrorMessage(cause, t('channelCards.loadFailed'))
      schedule(60)
    } finally {
      if (request === controller) loading.value = false
    }
  }
  function setRange(value: MonitorRange) { if (range.value !== value) { range.value = value; void reload() } }
  async function loadRates() {
    try {
      const [groups, custom] = await Promise.all([userGroupsAPI.getAvailable(), userGroupsAPI.getUserGroupRates()])
      if (!stopped) rates.value = Object.fromEntries(groups.map(group => [group.id, custom[group.id] ?? group.rate_multiplier]))
    } catch { /* Optional pricing must not hide monitoring. */ }
  }
  function visible() { if (!document.hidden && Date.now() >= refreshAt && !loading.value) void reload() }
  onMounted(() => {
    void reload()
    void loadRates()
    tickTimer = setInterval(() => { countdown.value = Math.max(0, Math.ceil((refreshAt - Date.now()) / 1000)) }, 1000)
    document.addEventListener('visibilitychange', visible)
  })
  onBeforeUnmount(() => {
    stopped = true
    request?.abort()
    clearTimeout(refreshTimer)
    clearInterval(tickTimer)
    document.removeEventListener('visibilitychange', visible)
  })
  return { range, snapshot, matrix, loading, error, rates, countdown, sections, timelineLength, reload, setRange }
}
