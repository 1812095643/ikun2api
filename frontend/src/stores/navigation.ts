import { defineStore } from 'pinia'
import { shallowRef } from 'vue'
import { navigationAPI, type NavigationBadge, type NavigationSection, type NavigationStatus } from '@/api/navigation'
import { getWorkspaceModels } from '@/api/modelPlaza'
import { announcementsAPI } from '@/api'
import { useAuthStore } from './auth'

const sectionPaths: Record<string, NavigationSection> = {
  '/news': 'news', '/reset-monitor': 'reset', '/model-square': 'models',
  '/lottery': 'lottery', '/admin/lottery': 'admin_lottery'
}

export const NAVIGATION_READ_SIGNAL = 'ikun-navigation-read-synced:'
const PENDING_READ_PREFIX = 'ikun-navigation-pending:'
const readSections: NavigationSection[] = ['news', 'reset', 'models', 'lottery', 'admin_lottery', 'pages']
type ReadKeys = NavigationStatus['seen']

function loadPendingReads(userID: number): ReadKeys {
  if (!userID) return {}
  try {
    const value = JSON.parse(localStorage.getItem(`${PENDING_READ_PREFIX}${userID}`) || '{}')
    if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
    const result: ReadKeys = {}
    for (const section of readSections) {
      if (Array.isArray(value[section])) result[section] = [...new Set<string>(value[section].filter((key: unknown) =>
        typeof key === 'string' && key.trim() && key.length <= 256 && !key.includes('\0') && !key.includes('\r') && !key.includes('\n')
      ))].slice(-4096)
    }
    return result
  } catch {
    return {}
  }
}

export function unreadKeys(current: string[] = [], seen: string[] = []): number {
  const read = new Set(seen)
  return new Set(current.filter(key => !read.has(key))).size
}

function mergeSeen(previous: NavigationStatus['seen'], next: NavigationStatus['seen']): NavigationStatus['seen'] {
  const result = { ...previous }
  for (const section of Object.keys(next) as NavigationSection[]) {
    result[section] = [...new Set([...(result[section] || []), ...(next[section] || [])])].slice(-4096)
  }
  return result
}

export const useNavigationStore = defineStore('navigation', () => {
  const auth = useAuthStore()
  const personal = shallowRef<NavigationStatus | null>(null)
  const management = shallowRef<NavigationStatus | null>(null)
  const seen = shallowRef<Partial<Record<NavigationSection, string[]>>>({})
  const modelKeys = shallowRef<string[]>([])
  const pageVersions = shallowRef<Record<string, string>>({})
  const announcementUnread = shallowRef(0)
  const pageTrackingReady = shallowRef(false)
  let owner = 0
  let generation = 0
  let pending: Promise<void> | null = null
  let modelFetchedAt = 0
  let fetchedAt = 0
  let versionsLoaded = false
  let versionsFetchedAt = 0
  let loadedBuild = ''
  let queuedReads: ReadKeys = {}
  let writing: Promise<void> | null = null

  function reset() {
    owner = auth.user?.id || 0
    generation++
    pending = null
    personal.value = null
    management.value = null
    seen.value = {}
    modelKeys.value = []
    announcementUnread.value = 0
    pageTrackingReady.value = false
    modelFetchedAt = 0
    fetchedAt = 0
    queuedReads = loadPendingReads(owner)
    writing = null
  }

  function savePendingReads() {
    if (!owner) return
    try {
      const key = `${PENDING_READ_PREFIX}${owner}`
      if (Object.values(queuedReads).some(keys => keys?.length)) localStorage.setItem(key, JSON.stringify(queuedReads))
      else localStorage.removeItem(key)
    } catch {
      // 禁用存储时仍保留内存队列，当前会话继续重试。
    }
  }

  function removeAcknowledgedReads() {
    for (const section of readSections) {
      queuedReads[section] = queuedReads[section]?.filter(key => !seen.value[section]?.includes(key))
    }
    savePendingReads()
  }

  async function flushReads() {
    if (writing) return writing
    if (!auth.user || owner !== auth.user.id) return
    if (!Object.values(queuedReads).some(keys => keys?.length)) return
    const current = generation
    writing = (async () => {
      try {
        // 同一账号串行提交；刷新中断后从本地队列恢复，服务端确认后才清除提醒。
        while (current === generation && owner === auth.user?.id) {
          const section = readSections.find(value => queuedReads[value]?.length)
          if (!section) break
          const batch = queuedReads[section]!.slice(0, 100)
          const result = await navigationAPI.read(section, batch, section === 'admin_lottery')
          if (current !== generation || owner !== auth.user?.id) return
          if (!batch.every(key => result.keys.includes(key))) throw new Error('Read acknowledgment incomplete')
          seen.value = mergeSeen(seen.value, { [section]: result.keys })
          removeAcknowledgedReads()
          try {
            localStorage.setItem(`${NAVIGATION_READ_SIGNAL}${owner}`, `${Date.now()}:${Math.random()}`)
          } catch {
            // 跨窗口信号不可用时，由聚焦和定时刷新同步服务器状态。
          }
        }
      } catch {
        // 保留待保存的键；联网、聚焦或下次刷新时重试，不能将失败伪装为已读。
      }
    })().finally(() => {
      if (current === generation) writing = null
    })
    return writing
  }

  async function refresh(force = false) {
    if (!auth.user || !auth.token) { reset(); return }
    if (owner !== auth.user.id) reset()
    if (pending) {
      if (!force) return pending
      const userID = auth.user.id
      await pending
      if (userID === auth.user?.id) { fetchedAt = 0; return refresh() }
      return
    }
    if (!force && Date.now() - fetchedAt < 8000) return
    const current = generation
    const canApply = () => current === generation && owner === auth.user?.id
    const tasks: Promise<unknown>[] = [
      navigationAPI.status().then(async value => {
        if (!canApply()) return
        personal.value = value
        seen.value = mergeSeen(seen.value, value.seen)
        removeAcknowledgedReads()
        await flushReads()
      }),
      announcementsAPI.list(true).then(items => {
        if (canApply()) announcementUnread.value = items.filter(item => !item.read_at).length
      })
    ]
    if (auth.isAdmin) tasks.push(navigationAPI.status(true).then(value => {
      if (!canApply()) return
      management.value = value
      seen.value = mergeSeen(seen.value, value.seen)
    }))
    if (Date.now() - modelFetchedAt > 300000) tasks.push(getWorkspaceModels().then(value => {
      if (!canApply()) return
      modelKeys.value = [...new Set(value.groups.flatMap(group => group.models.map(model => `${model.platform || group.platform}:${model.name}`)))].sort()
      modelFetchedAt = Date.now()
    }))
    if (!versionsLoaded || Date.now() - versionsFetchedAt > 300000) tasks.push(fetch('/navigation-pages.json', { cache: 'no-store' }).then(async response => {
      if (!response.ok) return
      const value: { build: string; pages: Record<string, string> } = await response.json()
      pageVersions.value = value.pages
      versionsLoaded = true
      loadedBuild = value.build
      versionsFetchedAt = Date.now()
    }))
    pending = Promise.allSettled(tasks).then(async results => {
      if (!canApply()) return
      fetchedAt = Date.now()
      if (results[0].status === 'fulfilled' && versionsLoaded && !pageTrackingReady.value) {
        // 首次接入建立当前页面基线，后续仅提醒发生变化的页面。
        if (!seen.value.pages?.length && loadedBuild === __NAVIGATION_BUILD_ID__) await markKeys('pages', Object.entries(pageVersions.value).map(([path, version]) => `${path}:${version}`))
        if (canApply()) pageTrackingReady.value = !!seen.value.pages?.length || loadedBuild !== __NAVIGATION_BUILD_ID__
      }
    }).finally(() => {
      if (current === generation) pending = null
    })
    return pending
  }

  function sourceKeys(section: NavigationSection): string[] {
    if (section === 'news' || section === 'reset') return personal.value?.[section] || []
    if (section === 'models') return modelKeys.value
    return (section === 'admin_lottery' ? management.value : personal.value)?.activities || []
  }

  async function markKeys(section: NavigationSection, keys: string[]) {
    if (!auth.user || owner !== auth.user.id || !keys.length) return
    const next = keys.filter(key => !seen.value[section]?.includes(key))
    if (!next.length) return
    queuedReads = mergeSeen(queuedReads, { [section]: next })
    savePendingReads()
    await flushReads()
  }

  function pageKey(path: string): string | undefined {
    return pageVersions.value[path] ? `${path}:${pageVersions.value[path]}` : undefined
  }

  function badge(path: string): NavigationBadge {
    // 管理菜单仅提醒工单未读，页面更新和新内容提醒用于用户菜单。
    if (path === '/admin' || path.startsWith('/admin/')) {
      if (path !== '/admin/tickets' || !auth.isAdmin || owner !== auth.user?.id) return { count: 0, dot: false, label: '' }
      const count = management.value?.tickets || 0
      return { count, dot: false, label: count ? `${count} 条未读消息` : '' }
    }
    // 模型列表、版本清单可能先返回；等服务端已读记录恢复后再计算，避免先亮后灭。
    if (!personal.value || owner !== auth.user?.id) return { count: 0, dot: false, label: '' }
    const section = sectionPaths[path]
    let count = section ? unreadKeys(sourceKeys(section), seen.value[section]) : 0
    let label = count ? `${count} 条新内容` : ''
    if (path === '/tickets') {
      count = personal.value?.tickets || 0
      label = `${count} 条未读消息`
    } else if (path === '/announcements') {
      count = announcementUnread.value
      label = `${count} 条未读公告`
    } else if (path === '/lottery') {
      count += personal.value?.pending_wins || 0
      label = `${count} 项新活动或待领奖`
    } else if (path === '/checkin') {
      count = personal.value?.unopened_boxes || 0
      label = count ? `${count} 份盲盒待揭晓` : personal.value?.checkin_needed ? '今日未签到' : ''
    }
    const version = pageKey(path)
    const updated = pageTrackingReady.value && !!version && !seen.value.pages?.includes(version)
    const dot = updated || (path === '/checkin' && !!personal.value?.checkin_needed)
    if (updated) label = label ? `${label}，页面有更新` : '页面有更新'
    return { count, dot, label }
  }

  async function markPage(path: string) {
    const userID = auth.user?.id
    if (!versionsLoaded || owner !== userID) await refresh()
    if (userID !== auth.user?.id) return
    if (loadedBuild !== __NAVIGATION_BUILD_ID__) return
    const key = pageKey(path)
    if (key) await markKeys('pages', [key])
  }

  async function viewed(path: string, keys?: string[]) {
    const userID = auth.user?.id
    if (userID && owner !== userID) reset()
    const section = sectionPaths[path]
    // 页面已返回的内容立即保存，不等待无关的模型、公告或版本请求完成。
    if (section && keys) await markKeys(section, keys)
    await refresh()
    if (userID !== auth.user?.id) return
    if (section && !keys) await markKeys(section, sourceKeys(section))
    await markPage(path)
  }

  return { personal, management, seen, pageVersions, announcementUnread, refresh, reset, badge, viewed, markPage }
})
