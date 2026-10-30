import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h, nextTick, ref } from 'vue'
import { createMemoryHistory, createRouter, RouterLink, RouterView, type Router } from 'vue-router'
import zh from '@/i18n/locales/zh'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { useOnboardingStore } from '@/stores/onboarding'
import { getUserSteps } from '@/components/Guide/userSteps'
import type { PublicSettings, User } from '@/types'
import { useOnboardingTour } from '../useOnboardingTour'

vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({
    t: (key: string) => key.split('.').reduce<unknown>((value, part) =>
      value && typeof value === 'object' ? (value as Record<string, unknown>)[part] : undefined, zh
    ) as string,
  }),
}))

describe('可操作的用户新手引导', () => {
  let wrapper: VueWrapper | undefined
  let tour: ReturnType<typeof useOnboardingTour>
  let router: Router
  let created: number

  async function settle() {
    await vi.advanceTimersByTimeAsync(500)
    for (let index = 0; index < 4; index++) await nextTick()
  }

  async function mountTour(role: 'admin' | 'user' = 'user', id = 21, paths = ['/model-square', '/keys', '/usage', '/tickets']) {
    useAuthStore().user = { id, role, email: `tour-${id}@example.com` } as User
    const Page = defineComponent({
      setup() {
        const open = ref(false)
        const onboarding = useOnboardingStore()
        onboarding.setKeyFormCloseCallback(() => { open.value = false })
        return () => h('main', { 'data-tour': 'user-page-content' }, [
          h('div', { class: 'catalog-toolbar' }),
          h('div', { 'data-tour': 'keys-connect-help' }, 'API 地址和密钥列表'),
          h('button', { 'data-tour': 'keys-create-btn', onClick: () => { open.value = true } }, '创建密钥'),
          open.value && h('div', { role: 'dialog' }, [
            h('input', { 'data-tour': 'key-form-name' }),
            h('div', { 'data-tour': 'key-form-provider' }, h('button', 'Claude')),
            h('div', { 'data-tour': 'key-form-group' }, h('button', { onClick: () => onboarding.completeAction('key-group-selected') }, '选择分组')),
            h('button', { 'data-tour': 'key-form-submit', onClick: () => { created++ } }, '创建'),
          ]),
        ])
      },
    })
    router = createRouter({ history: createMemoryHistory(), routes: [...paths, '/admin/dashboard'].map(path => ({ path, component: Page, meta: { requiresAuth: true } })) })
    await router.push(role === 'admin' ? '/admin/dashboard' : paths[0])
    await router.isReady()
    wrapper = mount(defineComponent({
      setup() {
        tour = useOnboardingTour({ autoStart: true })
        return () => h('div', [
          ...paths.map(path => h(RouterLink, { to: path, 'data-tour-path': path }, { default: () => h('span', { class: 'sidebar-label' }, path) })),
          h('a', { 'data-tour': 'header-tutorial', href: '/tutorial', target: '_blank' }, '新手教程'),
          h(RouterView),
        ])
      },
    }), { attachTo: document.body, global: { plugins: [router] } })
  }

  async function clickNext() {
    document.querySelector<HTMLButtonElement>('.driver-popover-next-btn')!.click()
    await settle()
  }

  async function reach(selector: string) {
    for (let index = 0; index < 30; index++) {
      const active = useOnboardingStore().getDriverInstance()!
      if (active.getActiveStep()?.element === selector) return
      const step = active.getActiveStep() as { action?: string }
      if (step.action === 'click') (active.getActiveElement() as HTMLElement).click()
      else if (step.action === 'group') useOnboardingStore().completeAction('key-group-selected')
      else document.querySelector<HTMLButtonElement>('.driver-popover-next-btn')!.click()
      await settle()
    }
    throw new Error(`未到达步骤：${selector}，当前步骤：${useOnboardingStore().getDriverInstance()?.getActiveStep()?.element}`)
  }

  beforeEach(() => {
    created = 0
    setActivePinia(createPinia())
    localStorage.clear()
    vi.useFakeTimers()
    vi.stubGlobal('innerWidth', 1280)
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 160, 40))
    Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() })
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('管理员不自动弹出，可手动预览用户实际流程', async () => {
    await mountTour('admin')
    await vi.advanceTimersByTimeAsync(1100)
    expect(useOnboardingStore().isDriverActive()).toBe(false)
    await tour.startTour()
    expect(useOnboardingStore().isDriverActive()).toBe(true)
    expect(useOnboardingStore().userPreview).toBe(true)
    expect(document.querySelector('.driver-popover-title')?.textContent).toContain('IKUN2api')
    expect(useOnboardingStore().getDriverInstance()!.getConfig().steps?.map(step => step.element)).not.toContain('#sidebar-group-manage')
  })

  it('后台模式下管理员可手动预览，普通用户不能启动', async () => {
    useAppStore().cachedPublicSettings = { backend_mode_enabled: true } as PublicSettings
    await mountTour('admin')
    await tour.startTour()
    expect(useOnboardingStore().isDriverActive()).toBe(true)
    wrapper!.unmount()
    await mountTour('user')
    await tour.startTour()
    expect(useOnboardingStore().isDriverActive()).toBe(false)
  })

  it('新用户自动启动，仅按实际可见菜单保留整组后续步骤', async () => {
    await mountTour()
    await vi.advanceTimersByTimeAsync(1100)
    const steps = useOnboardingStore().getDriverInstance()!.getConfig().steps!
    expect(steps.some(step => step.element === '[data-tour="key-form-group"]')).toBe(true)
    expect(steps.some(step => step.element === '[data-tour-path="/purchase"]')).toBe(false)
    expect(useOnboardingStore().getDriverInstance()!.getConfig().disableActiveInteraction).toBe(false)
  })

  it('必须点击入口，切页后保留同一实例并高亮页面内容', async () => {
    await mountTour('admin')
    await tour.startTour()
    const driver = useOnboardingStore().getDriverInstance()!
    await clickNext()
    const before = driver.getActiveIndex()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    await settle()
    expect(driver.getActiveIndex()).toBe(before)
    document.querySelector<HTMLElement>('[data-tour-path="/model-square"]')!.click()
    await settle()
    expect(router.currentRoute.value.path).toBe('/model-square')
    expect(useOnboardingStore().getDriverInstance()).toBe(driver)
    expect(driver.getActiveStep()?.element).toBe('.catalog-toolbar')
    expect(document.querySelectorAll('.driver-active-element')).toHaveLength(1)
  })

  it('点击创建按钮后进入真实表单，选分组前不能跳过', async () => {
    await mountTour('admin')
    await tour.startTour()
    await reach('[data-tour="key-form-group"]')
    expect(router.currentRoute.value.path).toBe('/keys')
    expect(document.querySelector('main [role="dialog"]')).not.toBeNull()
    const driver = useOnboardingStore().getDriverInstance()!
    const before = driver.getActiveIndex()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    await settle()
    expect(driver.getActiveIndex()).toBe(before)
    document.querySelector<HTMLButtonElement>('[data-tour="key-form-group"] button')!.click()
    await settle()
    expect(driver.getActiveStep()?.element).toBe('[data-tour="key-form-submit"]')
    expect(document.querySelectorAll('.driver-active-element')).toHaveLength(1)
    expect(created).toBe(0)
  })

  it('可以只了解创建与导入，不会自动生成密钥', async () => {
    await mountTour('admin')
    await tour.startTour()
    await reach('[data-tour="key-form-submit"]')
    await clickNext()
    expect(created).toBe(0)
    expect(document.querySelector('main [role="dialog"]')).toBeNull()
    expect(useOnboardingStore().getDriverInstance()!.getActiveStep()?.element).toBe('[data-tour="keys-connect-help"]')
  })

  it('弹窗内部滚动也同步高亮，退出后解除监听', async () => {
    await mountTour('admin')
    await tour.startTour()
    await reach('[data-tour="key-form-group"]')
    const driver = useOnboardingStore().getDriverInstance()!
    const refresh = vi.spyOn(driver, 'refresh')
    const panel = document.querySelector('main [role="dialog"]')!
    panel.dispatchEvent(new Event('scroll'))
    expect(refresh).toHaveBeenCalled()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    refresh.mockClear()
    panel.dispatchEvent(new Event('scroll'))
    expect(refresh).not.toHaveBeenCalled()
  })

  it('切页后返回上一步会恢复对应页面，退出不会留下表单', async () => {
    await mountTour('admin')
    await tour.startTour()
    await reach('[data-tour="keys-create-btn"]')
    const driver = useOnboardingStore().getDriverInstance()!
    document.querySelector<HTMLButtonElement>('.driver-popover-prev-btn')!.click()
    await settle()
    document.querySelector<HTMLButtonElement>('.driver-popover-prev-btn')!.click()
    await settle()
    expect(router.currentRoute.value.path).toBe('/model-square')
    expect(driver.getActiveStep()?.element).toBe('.catalog-toolbar')
    await reach('[data-tour="key-form-name"]')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(document.querySelector('main [role="dialog"]')).toBeNull()
    expect(useOnboardingStore().userPreview).toBe(false)
    expect(useOnboardingStore().isDriverActive()).toBe(false)
  })

  it('手机展开导航，进入页面后收起，退出恢复原布局', async () => {
    vi.stubGlobal('innerWidth', 390)
    const app = useAppStore()
    app.setSidebarCollapsed(true)
    await mountTour('admin')
    await tour.startTour()
    await clickNext()
    expect(app.mobileOpen).toBe(true)
    document.querySelector<HTMLElement>('[data-tour-path="/model-square"]')!.click()
    await settle()
    expect(app.mobileOpen).toBe(false)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(app.sidebarCollapsed).toBe(true)
    expect(app.mobileOpen).toBe(false)
  })

  it('沿用旧版已读状态，重播不会导致下次刷新自动弹出', async () => {
    localStorage.setItem('user_guide_21_user_v1_user_overview', 'true')
    await mountTour()
    await vi.advanceTimersByTimeAsync(1100)
    expect(useOnboardingStore().isDriverActive()).toBe(false)
    await tour.startTour()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    wrapper!.unmount()
    await mountTour()
    await vi.advanceTimersByTimeAsync(1100)
    expect(useOnboardingStore().isDriverActive()).toBe(false)
  })

  it('主动离开当前流程会清理浮层，已读状态按账号隔离', async () => {
    await mountTour()
    await tour.startTour()
    await router.push('/tickets')
    await settle()
    expect(useOnboardingStore().isDriverActive()).toBe(false)
    expect(document.querySelector('.driver-popover')).toBeNull()
    wrapper!.unmount()
    await mountTour('user', 22)
    await vi.advanceTimersByTimeAsync(1100)
    expect(useOnboardingStore().isDriverActive()).toBe(true)
  })

  it('用户标准页面均有进入和内容步骤，条件关闭的功能不会出现', async () => {
    const paths = ['/dashboard', '/model-square', '/purchase', '/redeem', '/keys', '/usage', '/available-channels', '/monitor', '/subscriptions', '/orders', '/affiliate', '/checkin', '/lottery', '/tickets', '/news', '/reset-monitor', '/batch-image', '/profile']
    await mountTour('admin', 1, paths)
    const steps = getUserSteps(key => key)
    for (const path of paths) {
      expect(steps.filter(step => step.route === path).length).toBeGreaterThanOrEqual(2)
    }
    expect(steps.some(step => step.element === '[data-tour="keys-connect-help"]')).toBe(true)
  })
})
