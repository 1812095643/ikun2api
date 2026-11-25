import { onBeforeUnmount, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { NAVIGATION_READ_SIGNAL, useNavigationStore } from '@/stores/navigation'
import { NAVIGATION_CHANGED } from '@/utils/navigationEvents'
import { onLayoutPageChange } from '@/components/layout/layoutShell'

// 切回标签页时 visibilitychange 与 focus 几乎同时触发，合并为一次强制刷新。
const AUTO_REFRESH_GAP_MS = 2000

export function useSidebarNotices() {
  const store = useNavigationStore()
  const auth = useAuthStore()
  const route = useRoute()
  let timer: ReturnType<typeof setInterval> | undefined
  let pageTimer: ReturnType<typeof setTimeout> | undefined
  let stopped = false

  function canonicalPath() {
    const base = route.path.replace(/\/$/, '')
    return base.startsWith('/tutorial/') ? '/tutorial' : base.replace(/^(\/admin\/tickets|\/tickets)\/\d+$/, '$1')
  }

  let lastAutoRefresh = 0

  // 用户操作（已读、签到、工单等）后的通知必须拿到操作之后的数据，始终强制刷新。
  function onChanged() {
    if (!document.hidden) void store.refresh(true)
  }

  // 可见、聚焦、联网与定时器只是“可能过期”的信号，短时间内只刷新一次。
  function onVisible() {
    if (document.hidden) return
    const now = Date.now()
    if (now - lastAutoRefresh < AUTO_REFRESH_GAP_MS) return
    lastAutoRefresh = now
    void store.refresh(true)
  }

  function onReadSynced(event: StorageEvent) {
    if (event.key === `${NAVIGATION_READ_SIGNAL}${auth.user?.id}`) onChanged()
  }

  watch(() => [auth.user?.id, auth.user?.role], () => { void store.refresh() }, { immediate: true })
  // 侧边栏常驻后不再随页面重建；保留原来每次进入新页面时的节流刷新（8 秒内复用）。
  onLayoutPageChange(() => { void store.refresh() })
  watch(() => route.path, () => {
    clearTimeout(pageTimer)
    const path = canonicalPath()
    // 页面版本在路由内容展示后确认；业务未读仍由各自成功加载或完成动作来消除。
    pageTimer = setTimeout(() => { if (!stopped && !document.hidden) void store.markPage(path) }, 1200)
  }, { immediate: true })

  onMounted(() => {
    timer = setInterval(onVisible, 30000)
    document.addEventListener('visibilitychange', onVisible)
    window.addEventListener('focus', onVisible)
    window.addEventListener('online', onVisible)
    window.addEventListener('storage', onReadSynced)
    window.addEventListener(NAVIGATION_CHANGED, onChanged)
  })
  onBeforeUnmount(() => {
    stopped = true
    clearInterval(timer)
    clearTimeout(pageTimer)
    document.removeEventListener('visibilitychange', onVisible)
    window.removeEventListener('focus', onVisible)
    window.removeEventListener('online', onVisible)
    window.removeEventListener('storage', onReadSynced)
    window.removeEventListener(NAVIGATION_CHANGED, onChanged)
  })
  return store
}
