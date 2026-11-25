/**
 * 路由预加载组合式函数
 * 在浏览器空闲时预加载可能访问的下一个页面，提升导航体验
 *
 * 优化说明：
 * - 不使用静态 import() 映射表，避免增加入口文件大小
 * - 通过路由配置动态获取组件的 import 函数
 * - 只在实际需要预加载时才执行
 */
import { ref, readonly } from 'vue'
import type { RouteLocationNormalized, Router } from 'vue-router'

/**
 * 组件导入函数类型
 */
type ComponentImportFn = () => Promise<unknown>

/**
 * 预加载邻接表：定义每个路由应该预加载哪些相邻路由
 * 只存储路由路径，不存储 import 函数，避免打包问题
 */
const PREFETCH_ADJACENCY: Record<string, string[]> = {
  // Admin routes - 预加载最常访问的相邻页面
  '/admin/dashboard': ['/admin/accounts', '/admin/users'],
  '/admin/accounts': ['/admin/dashboard', '/admin/users'],
  '/admin/users': ['/admin/groups', '/admin/dashboard'],
  '/admin/groups': ['/admin/subscriptions', '/admin/users'],
  '/admin/subscriptions': ['/admin/groups', '/admin/redeem'],
  // User routes
  '/dashboard': ['/keys', '/usage'],
  '/keys': ['/dashboard', '/usage'],
  '/usage': ['/keys', '/redeem'],
  '/redeem': ['/usage', '/profile'],
  '/profile': ['/dashboard', '/keys']
}

/**
 * requestIdleCallback 的返回类型
 */
type IdleCallbackHandle = number | ReturnType<typeof setTimeout>

/**
 * requestIdleCallback polyfill (Safari < 15)
 */
const scheduleIdleCallback = (
  callback: IdleRequestCallback,
  options?: IdleRequestOptions
): IdleCallbackHandle => {
  if (typeof window.requestIdleCallback === 'function') {
    return window.requestIdleCallback(callback, options)
  }
  return setTimeout(() => {
    callback({ didTimeout: false, timeRemaining: () => 50 })
  }, 1000)
}

const cancelScheduledCallback = (handle: IdleCallbackHandle): void => {
  if (typeof window.cancelIdleCallback === 'function' && typeof handle === 'number') {
    window.cancelIdleCallback(handle)
  } else {
    clearTimeout(handle)
  }
}

/**
 * 路由预加载组合式函数
 *
 * @param router - Vue Router 实例，用于获取路由组件
 */
export function useRoutePrefetch(router?: Router) {
  // 当前挂起的预加载任务句柄
  const pendingPrefetchHandle = ref<IdleCallbackHandle | null>(null)

  // 已预加载的路由集合
  const prefetchedRoutes = ref<Set<string>>(new Set())

  /**
   * 从路由配置中获取组件的 import 函数
   */
  const getComponentImporter = (path: string): ComponentImportFn | null => {
    if (!router) return null

    const routes = router.getRoutes()
    const route = routes.find((r) => r.path === path)

    if (route && route.components?.default) {
      const component = route.components.default
      // 检查是否是懒加载组件（函数形式）
      if (typeof component === 'function') {
        return component as ComponentImportFn
      }
    }
    return null
  }

  /**
   * 获取当前路由应该预加载的路由路径列表
   */
  const getPrefetchPaths = (route: RouteLocationNormalized): string[] => {
    return PREFETCH_ADJACENCY[route.path] || []
  }

  /**
   * 执行单个组件的预加载
   */
  const prefetchComponent = async (importFn: ComponentImportFn): Promise<void> => {
    try {
      await importFn()
    } catch (error) {
      // 静默处理预加载错误
      if (import.meta.env.DEV) {
        console.debug('[Prefetch] Failed to prefetch component:', error)
      }
    }
  }

  /**
   * 取消挂起的预加载任务
   */
  const cancelPendingPrefetch = (): void => {
    if (pendingPrefetchHandle.value !== null) {
      cancelScheduledCallback(pendingPrefetchHandle.value)
      pendingPrefetchHandle.value = null
    }
  }

  /**
   * 触发路由预加载
   */
  const triggerPrefetch = (route: RouteLocationNormalized): void => {
    cancelPendingPrefetch()

    const prefetchPaths = getPrefetchPaths(route)
    if (prefetchPaths.length === 0) return

    pendingPrefetchHandle.value = scheduleIdleCallback(
      () => {
        pendingPrefetchHandle.value = null

        const routePath = route.path
        if (prefetchedRoutes.value.has(routePath)) return

        // 获取需要预加载的组件 import 函数
        const importFns: ComponentImportFn[] = []
        for (const path of prefetchPaths) {
          const importFn = getComponentImporter(path)
          if (importFn) {
            importFns.push(importFn)
          }
        }

        if (importFns.length > 0) {
          Promise.all(importFns.map(prefetchComponent)).then(() => {
            prefetchedRoutes.value.add(routePath)
          })
        }
      },
      { timeout: 2000 }
    )
  }

  /**
   * 重置预加载状态
   */
  const resetPrefetchState = (): void => {
    cancelPendingPrefetch()
    prefetchedRoutes.value.clear()
  }

  /**
   * 判断是否为管理员路由
   */
  const isAdminRoute = (path: string): boolean => {
    return path.startsWith('/admin')
  }

  /**
   * 获取预加载配置（兼容旧 API）
   */
  const getPrefetchConfig = (route: RouteLocationNormalized): ComponentImportFn[] => {
    const paths = getPrefetchPaths(route)
    const importFns: ComponentImportFn[] = []
    for (const path of paths) {
      const importFn = getComponentImporter(path)
      if (importFn) importFns.push(importFn)
    }
    return importFns
  }

  return {
    prefetchedRoutes: readonly(prefetchedRoutes),
    triggerPrefetch,
    cancelPendingPrefetch,
    resetPrefetchState,
    _getPrefetchConfig: getPrefetchConfig,
    _isAdminRoute: isAdminRoute
  }
}

// 兼容旧测试的导出
export const _adminPrefetchMap = PREFETCH_ADJACENCY
export const _userPrefetchMap = PREFETCH_ADJACENCY

type NetworkInformationLike = { saveData?: boolean; effectiveType?: string }

const menuPrefetchQueue: ComponentImportFn[] = []
let menuPrefetchSeen = new WeakSet<ComponentImportFn>()
let menuPrefetchGeneration = 0
let menuPrefetchRunning = false

function menuPrefetchAllowed(): boolean {
  const connection = (navigator as Navigator & { connection?: NetworkInformationLike }).connection
  if (connection?.saveData) return false
  return !['slow-2g', '2g'].includes(connection?.effectiveType || '')
}

function runMenuPrefetch(): void {
  const next = menuPrefetchQueue.shift()
  if (!next) {
    menuPrefetchRunning = false
    return
  }
  menuPrefetchRunning = true
  const generation = menuPrefetchGeneration
  scheduleIdleCallback(() => {
    if (generation !== menuPrefetchGeneration) return
    if (!menuPrefetchAllowed()) { cancelMenuPrefetch(); return }
    // 单个失败静默跳过；部署后旧 chunk 不存在时，真正导航仍由 router.onError 处理。
    Promise.resolve().then(next).catch(() => undefined).finally(() => { if (generation === menuPrefetchGeneration) runMenuPrefetch() })
  }, { timeout: 3000 })
}

/**
 * 空闲时逐个预加载菜单能到达的页面代码，首次点开任意菜单不必再等待下载。
 * 只取尚未加载的懒加载组件（vue-router 加载后会把函数替换为组件对象）；省流量或 2G 网络下不预加载。
 */
export function prefetchRoutePaths(router: Router, paths: string[]): void {
  if (!menuPrefetchAllowed()) return
  for (const path of paths) {
    let matched
    try {
      matched = router.resolve(path).matched
    } catch {
      continue
    }
    for (const record of matched) {
      const component = record.components?.default
      if (typeof component !== 'function') continue
      const importFn = component as ComponentImportFn
      if (menuPrefetchSeen.has(importFn)) continue
      menuPrefetchSeen.add(importFn)
      menuPrefetchQueue.push(importFn)
    }
  }
  if (!menuPrefetchRunning) runMenuPrefetch()
}

export function cancelMenuPrefetch(): void {
  menuPrefetchGeneration++
  menuPrefetchQueue.length = 0
  menuPrefetchSeen = new WeakSet<ComponentImportFn>()
  menuPrefetchRunning = false
}
