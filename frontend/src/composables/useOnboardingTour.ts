import { nextTick, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { driver, type Driver } from 'driver.js'
import 'driver.js/dist/driver.css'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { useOnboardingStore } from '@/stores/onboarding'
import { getUserSteps, type UserTourStep } from '@/components/Guide/userSteps'

export interface OnboardingOptions {
  storageKey?: string
  autoStart?: boolean
}

export function useOnboardingTour(options: OnboardingOptions = {}) {
  const { t } = useI18n()
  const route = useRoute()
  const router = useRouter()
  const auth = useAuthStore()
  const app = useAppStore()
  const store = useOnboardingStore()
  let instance: Driver | null = null
  let steps: UserTourStep[] = []
  let cleanupInteraction: (() => void) | null = null
  let autoTimer: ReturnType<typeof setTimeout> | null = null
  let savedSidebar: { collapsed: boolean; mobileOpen: boolean } | null = null
  let disposed = false
  let moving = false
  let generation = 0
  let expectedRoute: string | null = null
  let tourOpenedAnnouncements = false

  function closeTourAnnouncements() {
    if (!tourOpenedAnnouncements) return
    window.dispatchEvent(new Event('ikun-tour-close-announcements'))
    tourOpenedAnnouncements = false
  }

  // 沿用既有已读键，升级引导不会让已关闭引导的用户再次自动弹出。
  const getStorageKey = () => `${options.storageKey ?? (auth.isAdmin ? 'admin_guide' : 'user_guide')}_${auth.user?.id ?? 'guest'}_${auth.user?.role ?? 'user'}_v1_user_overview`
  const hasSeen = () => localStorage.getItem(getStorageKey()) === 'true'
  const markAsSeen = () => localStorage.setItem(getStorageKey(), 'true')
  const clearSeen = () => localStorage.removeItem(getStorageKey())
  const canStart = () => auth.user?.role === 'admin' || (auth.user?.role === 'user' && !app.backendModeEnabled)
  const activeStep = () => steps[instance?.getActiveIndex() ?? -1]
  const isNavigation = (step: UserTourStep) => typeof step.element === 'string' && step.element.startsWith('[data-tour-path=')

  function cleanupTour() {
    generation++
    cleanupInteraction?.()
    cleanupInteraction = null
    store.closeKeyForm()
    closeTourAnnouncements()
    document.removeEventListener('keydown', onKeydown, true)
    document.removeEventListener('scroll', refreshTour, true)
    if (savedSidebar) {
      app.setSidebarCollapsed(savedSidebar.collapsed)
      app.setMobileOpen(savedSidebar.mobileOpen)
      savedSidebar = null
    }
    store.setUserPreview(false)
    store.setDriverInstance(null)
    instance = null
    expectedRoute = null
    moving = false
  }

  function stopTour() {
    if (!instance && !savedSidebar) return
    markAsSeen()
    instance?.destroy()
    if (instance || savedSidebar) cleanupTour()
  }

  async function waitForElement(selector: string, token: number, timeout = 8000): Promise<HTMLElement | null> {
    const deadline = Date.now() + timeout
    while (!disposed && token === generation && Date.now() < deadline) {
      const element = document.querySelector<HTMLElement>(selector)
      if (element && element.getBoundingClientRect().height > 0) return element
      await new Promise(resolve => setTimeout(resolve, 80))
    }
    return null
  }

  async function moveTo(index: number) {
    if (!instance || moving) return
    if (index >= steps.length) { stopTour(); return }
    if (index < 0) return
    moving = true
    const token = generation
    const step = steps[index]
    try {
      cleanupInteraction?.()
      cleanupInteraction = null
      if (!step.keyForm) store.closeKeyForm()
      if (!step.announcement) closeTourAnnouncements()
      const sidebar = isNavigation(step) || (typeof step.element === 'string' && step.element.includes('sidebar-'))
      if (sidebar) app.setSidebarCollapsed(false)
      app.setMobileOpen(sidebar && window.innerWidth < 1024)

      // 导航步骤必须由用户点击，回退到内容步骤才自动恢复对应页面。
      if (step.route && !isNavigation(step) && route.path !== step.route) {
        expectedRoute = step.route
        await router.push(step.route)
      }
      await nextTick()
      if (step.announcement && !step.action && !document.querySelector('[data-tour="announcements-panel"]')) {
        tourOpenedAnnouncements = true
        window.dispatchEvent(new Event('ikun-open-announcements'))
        await nextTick()
      }
      if (step.keyForm && !document.querySelector('[data-tour="key-form-name"]')) {
        document.querySelector<HTMLButtonElement>('[data-tour="keys-create-btn"]')?.click()
        await nextTick()
      }
      if (typeof step.element === 'string') {
        const element = await waitForElement(step.element, token)
        if (!element) {
          if (token === generation && !disposed) {
            app.showWarning('页面尚未准备好，请稍后从头像菜单重新开始引导。')
            stopTour()
          }
          return
        }
      }
      if (token !== generation || disposed || !instance) return
      instance.moveTo(index)
    } catch {
      if (token === generation) {
        app.showWarning('该页面暂时无法打开，请稍后从头像菜单重新开始引导。')
        stopTour()
      }
    } finally {
      if (token === generation) moving = false
    }
  }

  async function nextStep(delay = 0) {
    const index = instance?.getActiveIndex()
    const token = generation
    if (index === undefined) return
    if (delay) await new Promise(resolve => setTimeout(resolve, delay))
    if (token === generation && instance?.getActiveIndex() === index) await moveTo(index + 1)
  }

  const isCurrentStep = (selector: string) => instance?.isActive() === true && activeStep()?.element === selector

  function refreshTour() {
    if (instance?.isActive()) instance.refresh()
  }

  function bindInteraction(element: Element | undefined, step: UserTourStep) {
    cleanupInteraction?.()
    cleanupInteraction = null
    // 快速切步时上一段动画可能尚未结束，清掉遗留标记，保证始终只有一个高亮。
    document.querySelectorAll('.driver-active-element').forEach(previous => {
      previous.classList.remove('driver-active-element', 'driver-no-interaction')
      if (previous.getAttribute('aria-controls') === 'driver-popover-content') {
        previous.removeAttribute('aria-controls')
        previous.removeAttribute('aria-expanded')
        previous.removeAttribute('aria-haspopup')
      }
    })
    if (step.action !== 'click' || !element) return
    const index = instance?.getActiveIndex()
    const token = generation
    const onClick = async () => {
      if (step.announcement) tourOpenedAnnouncements = true
      await nextTick()
      if (token !== generation || instance?.getActiveIndex() !== index || moving) return
      if (step.route && isNavigation(step)) {
        await router.isReady()
        if (route.path !== step.route) return
      }
      await nextStep()
    }
    element.addEventListener('click', onClick)
    cleanupInteraction = () => element.removeEventListener('click', onClick)
  }

  function onKeydown(event: KeyboardEvent) {
    if (!instance?.isActive()) return
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopImmediatePropagation()
      stopTour()
      return
    }
    const target = event.target instanceof Element ? event.target : null
    if (target?.closest('input, textarea, select, [contenteditable="true"], .select-dropdown-portal, .select-trigger')) return
    if (event.key === 'ArrowLeft') {
      event.preventDefault()
      void moveTo((instance.getActiveIndex() ?? 0) - 1)
    } else if (event.key === 'ArrowRight' || event.key === 'Enter') {
      // 交互步骤不能通过方向键跳过；回车保留给真实按钮的原生点击。
      if (activeStep()?.action || target?.closest('button, a')) return
      event.preventDefault()
      void nextStep()
    }
  }

  async function startTour(startIndex = 0) {
    if (!canStart() || disposed) return
    if (autoTimer) { clearTimeout(autoTimer); autoTimer = null }
    stopTour()
    const token = ++generation
    savedSidebar = { collapsed: app.sidebarCollapsed, mobileOpen: app.mobileOpen }
    store.setUserPreview(auth.isAdmin)
    app.setSidebarCollapsed(false)
    await nextTick()
    if (!await waitForElement('[data-tour-path]', token)) {
      if (token === generation) cleanupTour()
      return
    }
    steps = getUserSteps(t)
    if (token !== generation || disposed) return
    instance = driver({
      steps,
      showProgress: true,
      progressText: '{{current}} / {{total}}',
      // 跨页和弹窗需要先稳定定位，避免高亮动画与真实控件滚动互相抢占。
      animate: false,
      allowClose: true,
      overlayClickBehavior: () => {},
      allowKeyboardControl: false,
      disableActiveInteraction: false,
      showButtons: ['previous', 'next', 'close'],
      stagePadding: 5,
      popoverClass: 'theme-tour-popover',
      nextBtnText: t('common.next'),
      prevBtnText: t('common.back'),
      doneBtnText: t('onboarding.finishTour'),
      onNextClick: () => { if (!activeStep()?.action) void nextStep() },
      onPrevClick: () => { void moveTo((instance?.getActiveIndex() ?? 0) - 1) },
      onCloseClick: stopTour,
      onHighlightStarted: (element, step) => bindInteraction(element, step as UserTourStep),
      // driver 在回调返回后才更新当前元素，延后一轮再读取新的位置。
      onHighlighted: () => queueMicrotask(refreshTour),
      onPopoverRender: (popover, { state }) => {
        const step = steps[state.activeIndex ?? 0]
        const left = document.createElement('div')
        left.className = 'footer-left'
        left.appendChild(popover.progress)
        const hint = document.createElement('span')
        hint.className = 'footer-shortcuts'
        hint.textContent = step.action ? '点击高亮处继续' : 'Esc 退出'
        left.appendChild(hint)
        popover.footerButtons.classList.add('footer-right')
        popover.footer.replaceChildren(left, popover.footerButtons)
        popover.nextButton.classList.toggle('driver-popover-done-btn', state.activeIndex === steps.length - 1)
        popover.closeButton.setAttribute('aria-label', '退出新手引导')
      },
      onDestroyed: () => { markAsSeen(); cleanupTour() },
    })
    store.setDriverInstance(instance)
    document.addEventListener('keydown', onKeydown, true)
    // 弹窗的内部滚动不冒泡到 window，捕获后同步高亮孔位和说明浮层。
    document.addEventListener('scroll', refreshTour, true)
    instance.drive(0)
    if (startIndex > 0) await moveTo(startIndex)
  }

  function replayTour() {
    if (canStart()) void startTour()
  }

  function scheduleAutoStart() {
    if (autoTimer) { clearTimeout(autoTimer); autoTimer = null }
    if (!options.autoStart || auth.user?.role !== 'user' || app.backendModeEnabled || hasSeen() || instance) return
    if (!route.meta.requiresAuth || !document.querySelector('[data-tour-path]')) return
    autoTimer = setTimeout(() => { void startTour() }, 1000)
  }

  watch(() => route.path, async (path) => {
    if (instance?.isActive()) {
      const step = activeStep()
      if (path === expectedRoute) { expectedRoute = null; return }
      if (step && isNavigation(step) && step.route === path) { await nextStep(); return }
      if (step?.route !== path) stopTour()
    } else {
      await nextTick()
      scheduleAutoStart()
    }
  }, { flush: 'post' })

  onMounted(() => {
    store.setReplayCallback(replayTour)
    store.setControlMethods({ nextStep, isCurrentStep })
    store.setActionCallback(action => {
      if (action === 'key-group-selected' && activeStep()?.action === 'group') void nextStep()
    })
    scheduleAutoStart()
  })

  onUnmounted(() => {
    disposed = true
    if (autoTimer) clearTimeout(autoTimer)
    stopTour()
    store.setReplayCallback(null)
    store.clearControlMethods()
    store.setActionCallback(null)
  })

  return { startTour, replayTour, nextStep, isCurrentStep, hasSeen, markAsSeen, clearSeen }
}
