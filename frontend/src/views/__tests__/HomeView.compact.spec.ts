import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, RouterLinkStub } from '@vue/test-utils'

import HomeView from '../HomeView.vue'

const { appStore, authStore } = vi.hoisted(() => ({
  appStore: {
    cachedPublicSettings: {} as Record<string, unknown>,
    siteName: 'Fallback site',
    siteLogo: '',
    docUrl: '',
    publicSettingsLoaded: true,
    fetchPublicSettings: vi.fn(),
  },
  authStore: {
    isAuthenticated: false,
    isAdmin: false,
    user: null as { email?: string } | null,
    checkAuth: vi.fn(),
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => appStore,
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore,
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key === 'home.prism.tutorial' ? '新手教程' : key }),
  }
})

function mountHome(settings: Record<string, unknown> = {}) {
  appStore.cachedPublicSettings = {
    site_name: 'Test site',
    site_subtitle: 'Test subtitle',
    ...settings,
  }

  return mount(HomeView, {
    global: {
      stubs: {
        RouterLink: RouterLinkStub,
        LocaleSwitcher: { template: '<div data-testid="locale-switcher" />' },
        Icon: { template: '<span data-testid="icon" />' },
        PrismScene: { template: '<div data-testid="scene" />' },
      },
    },
  })
}

function compactDestination(wrapper: ReturnType<typeof mountHome>) {
  return wrapper.findAllComponents(RouterLinkStub).find(link => link.attributes('data-testid') === 'compact-entry')?.props('to')
}

function modelPlazaDestination(wrapper: ReturnType<typeof mountHome>) {
  return wrapper
    .findAllComponents(RouterLinkStub)
    .find((link) => link.props('to') === '/model-square')
    ?.props('to')
}

describe('HomeView compact mode', () => {
  beforeEach(() => {
    authStore.isAuthenticated = false
    authStore.isAdmin = false
    authStore.user = null
    authStore.checkAuth.mockClear()
    appStore.fetchPublicSettings.mockClear()
    localStorage.clear()
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: false } as MediaQueryList)
  })

  it('renders custom HTML ahead of compact mode', () => {
    const wrapper = mountHome({
      compact_home_enabled: true,
      home_content: '<section id="custom-home">Custom home</section>',
    })

    expect(wrapper.get('#custom-home').text()).toBe('Custom home')
    expect(wrapper.find('[data-testid="compact-home"]').exists()).toBe(false)
  })

  it('renders custom URL content ahead of compact mode', () => {
    const wrapper = mountHome({
      compact_home_enabled: true,
      home_content: ' https://example.com/home ',
    })

    expect(wrapper.get('iframe').attributes('src')).toBe('https://example.com/home')
    expect(wrapper.find('[data-testid="compact-home"]').exists()).toBe(false)
  })

  it('treats whitespace-only custom content as empty and selects compact mode', () => {
    const wrapper = mountHome({ compact_home_enabled: true, home_content: ' \n\t ' })

    expect(wrapper.get('[data-testid="compact-home"]').text()).toContain('Test site')
  })

  it('renders the legacy Sub2 homepage when selected', () => {
    const wrapper = mountHome({ home_mode: 'legacy' })
    expect(wrapper.find('[data-testid=\"legacy-home\"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid=\"prism-landing\"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('renders custom HTML only when the custom mode is selected', () => {
    const wrapper = mountHome({ home_mode: 'custom', home_content: '<section id=\"custom-home\">Custom</section>' })
    expect(wrapper.get('#custom-home').text()).toBe('Custom')
    wrapper.unmount()
  })

  it.each([undefined, false])('selects the default home when compact mode is %s', (enabled) => {
    const settings = enabled === undefined ? {} : { compact_home_enabled: enabled }
    const wrapper = mountHome(settings)

    expect(wrapper.find('[data-testid="compact-home"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="prism-landing"]').exists()).toBe(true)
  })

  it('links unauthenticated visitors to login', () => {
    expect(compactDestination(mountHome({ compact_home_enabled: true }))).toBe('/login')
  })

  it('links authenticated users to their dashboard', () => {
    authStore.isAuthenticated = true

    expect(compactDestination(mountHome({ compact_home_enabled: true }))).toBe('/dashboard')
  })

  it('links administrators to the admin dashboard', () => {
    authStore.isAuthenticated = true
    authStore.isAdmin = true

    const wrapper = mountHome({ compact_home_enabled: true })
    expect(compactDestination(wrapper)).toBe('/admin/dashboard')
    expect(authStore.checkAuth).toHaveBeenCalledOnce()
    expect(appStore.fetchPublicSettings).not.toHaveBeenCalled()
  })

  it('shows the native model square link to anonymous visitors', () => {
    const wrapper = mountHome({
      compact_home_enabled: true,
      model_plaza_enabled: false,
      model_plaza_require_auth: false,
    })

    expect(modelPlazaDestination(wrapper)).toBe('/model-square')
  })

  it('keeps the native model square link independent of the retired official plaza switch', () => {
    const wrapper = mountHome({
      compact_home_enabled: true,
      model_plaza_enabled: false,
      model_plaza_require_auth: true,
    })

    expect(modelPlazaDestination(wrapper)).toBe('/model-square')
  })

  it('keeps the native model square link for authenticated visitors', () => {
    authStore.isAuthenticated = true

    const wrapper = mountHome({
      compact_home_enabled: true,
      model_plaza_enabled: false,
      model_plaza_require_auth: true,
    })

    expect(modelPlazaDestination(wrapper)).toBe('/model-square')
  })

  it('shows the native model square link in the default home header', () => {
    const wrapper = mountHome({
      model_plaza_enabled: false,
      model_plaza_require_auth: false,
    })

    expect(modelPlazaDestination(wrapper)).toBe('/model-square')
  })

  it('hides the model square link when menu visibility is off', () => {
    const wrapper = mountHome({
      compact_home_enabled: true,
      model_plaza_enabled: false,
      model_plaza_require_auth: false,
      menu_visibility: { '/model-square': false },
    })

    expect(modelPlazaDestination(wrapper)).toBeUndefined()
  })

  it('keeps a working native model entry when the optional official plaza is disabled', () => {
    const wrapper = mountHome({ model_plaza_enabled: false })
    const targets = wrapper.findAllComponents(RouterLinkStub).map((link) => link.props('to'))
    expect(targets).toContain('/model-square')
    expect(targets).not.toContain('/model-plaza')
    wrapper.unmount()
  })

  it('uses the configured site name and logo in the landing header', () => {
    const wrapper = mountHome({ site_name: '示例站点', site_logo: '/custom-brand.png' })
    expect(wrapper.get('.landing-brand').text()).toBe('示例站点')
    expect(wrapper.get('.brand-logo').attributes('src')).toBe('/custom-brand.png')
    expect(wrapper.find('.theme-switch').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each(['dark', 'light'])('preserves the %s theme without showing a landing theme control', (theme) => {
    localStorage.setItem('theme', theme)
    const wrapper = mountHome()
    expect(document.documentElement.classList.contains('dark')).toBe(theme === 'dark')
    expect(wrapper.find('button[aria-label="home.switchToDark"]').exists()).toBe(false)
    expect(wrapper.find('button[aria-label="home.switchToLight"]').exists()).toBe(false)
    expect(wrapper.get('.brand-logo').attributes('src')).toBe('/logo.svg')
    wrapper.unmount()
  })

  it('opens the mobile navigation and closes it with Escape', async () => {
    const wrapper = mountHome()
    const button = wrapper.get('button[aria-controls="landing-mobile-links"]')
    expect(button.attributes('aria-expanded')).toBe('false')
    await button.trigger('click')
    expect(button.attributes('aria-expanded')).toBe('true')
    expect(wrapper.get('#landing-mobile-links a[href="/tutorial"]').attributes('target')).toBe('_blank')
    await wrapper.get('.mobile-menu').trigger('keydown', { key: 'Escape' })
    expect(button.attributes('aria-expanded')).toBe('false')
    expect(wrapper.find('#landing-mobile-links').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each([
    {},
    { compact_home_enabled: true },
    { home_content: '<section>欢迎使用</section>' },
    { home_content: 'https://example.com/home' },
  ])('不同欢迎页模式均提供独立教程入口：%s', (settings) => {
    const links = mountHome(settings).findAll('a[href="/tutorial"]')
    expect(links.length).toBeGreaterThan(0)
    for (const link of links) {
      expect(link.text()).toContain('新手教程')
      expect(link.attributes('target')).toBe('_blank')
      expect(link.attributes('rel')).toBe('noopener noreferrer')
    }
  })
})
