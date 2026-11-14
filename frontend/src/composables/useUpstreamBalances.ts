import { computed, onMounted, onUnmounted, shallowRef, watch } from 'vue'
import { upstreamBalancesAPI, type UpstreamBalance, type UpstreamBalanceDraft } from '@/api/upstreamBalances'
import { useAppStore } from '@/stores/app'
import { useUpstreamBalanceDisplay } from './useUpstreamBalanceDisplay'
import { mergeUpstreamOrder, orderUpstreamBalances, upstreamHasError } from '@/utils/upstreamBalanceList'

export function useUpstreamBalances() {
  const app = useAppStore()
  const presentation = useUpstreamBalanceDisplay()
  const { display, initializeDisplay } = presentation
  const items = shallowRef<UpstreamBalance[]>([])
  const loading = shallowRef(true)
  const error = shallowRef('')
  const search = shallowRef('')
  const filter = shallowRef('all')
  const visibility = shallowRef('visible')
  const page = shallowRef(1)
  const pageSize = shallowRef(20)
  const selectedIDs = shallowRef<string[]>([])
  const refreshing = shallowRef<string | null>(null)
  const saving = shallowRef(false)
  const dragging = shallowRef(false)
  let stopped = false
  let active: AbortController | null = null
  let timer: ReturnType<typeof setTimeout> | undefined
  const ordered = computed(() => orderUpstreamBalances(items.value, display.value))
  const hiddenSet = computed(() => new Set(display.value.hidden))
  const hiddenCount = computed(() => items.value.filter(item => hiddenSet.value.has(item.id)).length)
  const counts = computed(() => ({ low: items.value.filter(row => row.alert.low).length, error: items.value.filter(upstreamHasError).length, models: items.value.filter(row => (row.models?.pending || 0) > 0).length, monitored: items.value.filter(row => row.config.monitor_enabled).length }))
  const filtered = computed(() => {
    const query = search.value.trim().toLowerCase()
    return ordered.value.filter(row => (visibility.value === 'all' || hiddenSet.value.has(row.id) === (visibility.value === 'hidden')) &&
      (!query || `${row.name} ${row.url} ${row.accounts.map(a => a.name).join(' ')}`.toLowerCase().includes(query)) &&
      (filter.value === 'all' || (filter.value === 'models' && (row.models?.pending || 0) > 0) || (filter.value === 'low' && row.alert.low) || (filter.value === 'monitored' && row.config.monitor_enabled) || (filter.value === 'error' && upstreamHasError(row))))
  })
  const visible = computed(() => filtered.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value))
  const busy = computed(() => items.value.some(row => row.checking || row.queued || row.models?.checking || row.models?.queued))
  watch([search, filter, visibility, pageSize], () => { page.value = 1; selectedIDs.value = [] })
  watch(() => filtered.value.length, length => { page.value = Math.min(page.value, Math.max(1, Math.ceil(length / pageSize.value))) })
  function schedule() {
    clearTimeout(timer)
    if (!stopped) timer = setTimeout(() => { if (document.visibilityState !== 'hidden' && !dragging.value) void load(false); else schedule() }, busy.value ? 3000 : 30000)
  }
  function setDragging(value: boolean) { dragging.value = value; if (value) active?.abort() }
  function reorder(ids: string[]) {
    presentation.updateDisplay({ order: mergeUpstreamOrder(ordered.value.map(row => row.id), ids), sort_by: 'manual' })
  }
  function move(id: string, direction: number) {
    const ids = visible.value.map(row => row.id), index = ids.indexOf(id), next = index + direction
    if (index < 0 || next < 0 || next >= ids.length) return
    ;[ids[index], ids[next]] = [ids[next], ids[index]]
    reorder(ids)
  }
  function setHidden(ids: string[], hidden: boolean) {
    presentation.setHidden(ids, hidden)
    selectedIDs.value = selectedIDs.value.filter(id => !ids.includes(id))
  }
  function resetFilters() { search.value = ''; filter.value = 'all'; visibility.value = 'visible' }
  async function load(showLoading = true) {
    if (stopped) return
    active?.abort()
    const controller = new AbortController()
    active = controller
    if (showLoading) loading.value = true
    try {
      const data = await upstreamBalancesAPI.list(controller.signal)
      if (stopped || controller.signal.aborted) return
      initializeDisplay(data.display)
      items.value = data.items
      const ids = new Set(data.items.map(row => row.id))
      selectedIDs.value = selectedIDs.value.filter(id => ids.has(id))
      error.value = ''
    } catch {
      if (!controller.signal.aborted) error.value = '上游列表加载失败，请重试。'
    } finally {
      if (active === controller) { loading.value = false; schedule() }
    }
  }
  async function refresh(id?: string, ids?: string[]) {
    if (refreshing.value !== null || ids?.length === 0) return
    refreshing.value = id || 'all'
    try {
      const result = await upstreamBalancesAPI.refresh(id, ids)
      app.showSuccess(result.queued ? `已提交 ${result.queued} 个上游查询` : '正在查询或刚刚查询过，请稍候')
      await load(false)
    } catch { app.showError('无法发起余额查询，请重试。') }
    finally { refreshing.value = null }
  }
  async function save(id: string, draft: UpstreamBalanceDraft) {
    if (saving.value) return false
    saving.value = true
    try {
      await upstreamBalancesAPI.save(id, draft)
      app.showSuccess('上游设置已保存')
      await load(false)
      return true
    } catch (e) {
      app.showError((e as { message?: string }).message || '保存失败，请检查配置后重试。')
      return false
    } finally { saving.value = false }
  }
  onMounted(() => { void load() })
  onUnmounted(() => { stopped = true; clearTimeout(timer); active?.abort() })
  return { ...presentation, items, loading, error, search, filter, visibility, page, pageSize, selectedIDs, refreshing, saving, filtered, visible, busy, hiddenCount, counts, load, refresh, save, setHidden, reorder, move, setDragging, resetFilters }
}
