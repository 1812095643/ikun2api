import { onBeforeUnmount, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { NAVIGATION_READ_SIGNAL, useNavigationStore } from '@/stores/navigation'
import { NAVIGATION_CHANGED } from '@/utils/navigationEvents'

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

  function onVisible() {
    if (!document.hidden) void store.refresh(true)
  }

  function onReadSynced(event: StorageEvent) {
    if (event.key === `${NAVIGATION_READ_SIGNAL}${auth.user?.id}`) onVisible()
  }

  watch(() => [auth.user?.id, auth.user?.role], () => { void store.refresh() }, { immediate: true })
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
    window.addEventListener(NAVIGATION_CHANGED, onVisible)
  })
  onBeforeUnmount(() => {
    stopped = true
    clearInterval(timer)
    clearTimeout(pageTimer)
    document.removeEventListener('visibilitychange', onVisible)
    window.removeEventListener('focus', onVisible)
    window.removeEventListener('online', onVisible)
    window.removeEventListener('storage', onReadSynced)
    window.removeEventListener(NAVIGATION_CHANGED, onVisible)
  })
  return store
}
