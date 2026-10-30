import { computed, onMounted, onUnmounted, shallowRef, watch } from 'vue'
import { upstreamBalancesAPI, type UpstreamBalance, type UpstreamBalanceDraft } from '@/api/upstreamBalances'
import { useAppStore } from '@/stores/app'

export function useUpstreamBalances() {
  const app = useAppStore()
  const items = shallowRef<UpstreamBalance[]>([])
  const loading = shallowRef(true)
  const error = shallowRef('')
  const search = shallowRef('')
  const filter = shallowRef('all')
  const page = shallowRef(1)
  const pageSize = shallowRef(20)
  const refreshing = shallowRef<string | null>(null)
  const saving = shallowRef(false)
  let stopped = false
  let active: AbortController | null = null
  let timer: ReturnType<typeof setTimeout> | undefined
  const filtered = computed(() => {
    const query = search.value.trim().toLowerCase()
    return items.value.filter(row => (!query || `${row.name} ${row.url} ${row.accounts.map(a => a.name).join(' ')}`.toLowerCase().includes(query)) &&
      (filter.value === 'all' || (filter.value === 'low' && row.alert.low) || (filter.value === 'monitored' && row.config.monitor_enabled) ||
      (filter.value === 'error' && !!row.last_checked_at && (row.snapshot.successful < row.snapshot.total || !!row.snapshot.error))))
  })
  const visible = computed(() => filtered.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value))
  const busy = computed(() => items.value.some(row => row.checking || row.queued))
  watch([search, filter, pageSize], () => { page.value = 1 })
  function schedule() {
    clearTimeout(timer)
    if (!stopped) timer = setTimeout(() => { if (document.visibilityState !== 'hidden') void load(false); else schedule() }, busy.value ? 3000 : 30000)
  }
  async function load(showLoading = true) {
    active?.abort()
    const controller = new AbortController()
    active = controller
    if (showLoading) loading.value = true
    try {
      const data = await upstreamBalancesAPI.list(controller.signal)
      if (stopped || controller.signal.aborted) return
      items.value = data.items
      error.value = ''
      page.value = Math.min(page.value, Math.max(1, Math.ceil(filtered.value.length / pageSize.value)))
    } catch (e) {
      if (!controller.signal.aborted) error.value = '上游列表加载失败，请重试。'
    } finally {
      if (active === controller) { loading.value = false; schedule() }
    }
  }
  async function refresh(id?: string) {
    if (refreshing.value !== null) return
    refreshing.value = id || 'all'
    try {
      const result = await upstreamBalancesAPI.refresh(id)
      app.showSuccess(result.queued ? '已加入余额查询队列' : '正在查询或刚刚查询过，请稍候')
      await load(false)
    } catch { app.showError('无法发起余额查询，请重试。') }
    finally { refreshing.value = null }
  }
  async function save(id: string, draft: UpstreamBalanceDraft) {
    saving.value = true
    try {
      await upstreamBalancesAPI.save(id, draft)
      app.showSuccess('上游设置已保存')
      await load(false)
      return true
    } catch (e) {
      const failure = e as { message?: string }
      app.showError(failure.message || '保存失败，请检查配置后重试。')
      return false
    } finally { saving.value = false }
  }
  onMounted(() => { void load() })
  onUnmounted(() => { stopped = true; clearTimeout(timer); active?.abort() })
  return { items, loading, error, search, filter, page, pageSize, refreshing, saving, filtered, visible, busy, load, refresh, save }
}
