import { flushPromises, shallowMount } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PoolView from '../PoolView.vue'

const mocks = vi.hoisted(() => ({
  route: { path: '/pool', query: {} as Record<string, string> },
  auth: { isAdmin: false },
  replace: vi.fn(),
  overview: vi.fn(),
  accounts: vi.fn(),
  usage: vi.fn(),
  requests: vi.fn(),
  groups: vi.fn(),
  error: vi.fn(),
}))

vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('vue-router', () => ({
  useRoute: () => mocks.route,
  useRouter: () => ({ replace: mocks.replace }),
}))
vi.mock('@/stores', () => ({
  useAuthStore: () => mocks.auth,
  useAppStore: () => ({ showError: mocks.error }),
}))
vi.mock('@/api/pool', () => ({
  getPoolOverview: mocks.overview,
  listPoolAccounts: mocks.accounts,
  listPoolUsage: mocks.usage,
  listPoolAccountRequests: mocks.requests,
}))
vi.mock('@/api/admin/groups', () => ({ getAll: mocks.groups, default: { getAll: mocks.groups } }))
vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<div><slot /></div>' },
}))

function mountPage() {
  return shallowMount(PoolView, {
    global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
    } },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.route = reactive({ path: '/pool', query: {} as Record<string, string> })
  mocks.auth = reactive({ isAdmin: false })
  mocks.overview.mockResolvedValue({ account_count: 0, today: {}, window_7d: {}, generated_at: '2026-11-04T00:00:00Z' })
  mocks.accounts.mockResolvedValue({ items: [], total: 0 })
  mocks.usage.mockResolvedValue({ items: [], total: 0 })
  mocks.requests.mockResolvedValue({ items: [], total: 0 })
  mocks.groups.mockResolvedValue([])
})

describe('live account pool page', () => {
  it('loads user data and opens usage on the first tab click', async () => {
    const wrapper = mountPage()
    await flushPromises()
    expect(mocks.accounts).toHaveBeenCalledWith(expect.anything(), 'user', expect.anything())
    expect(mocks.groups).not.toHaveBeenCalled()
    expect(mocks.usage).not.toHaveBeenCalled()
    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    await flushPromises()
    expect(mocks.usage).toHaveBeenCalledTimes(1)
    expect(mocks.usage).toHaveBeenCalledWith(expect.not.objectContaining({ user_id: expect.anything() }), 'user', expect.anything())
    expect(mocks.replace).toHaveBeenCalledWith({ query: { tab: 'usage' } })
    await wrapper.findAll('[role="tab"]')[0].trigger('click')
    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    await flushPromises()
    expect(mocks.usage).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('loads a shared usage URL and follows query changes', async () => {
    mocks.route.query = { tab: 'usage' }
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.findAll('[role="tab"]')[1].attributes('aria-selected')).toBe('true')
    expect(mocks.usage).toHaveBeenCalledTimes(1)
    mocks.route.query = { tab: 'accounts' }
    await flushPromises()
    expect(wrapper.findAll('[role="tab"]')[0].attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('uses admin scope only in the protected admin route', async () => {
    mocks.auth.isAdmin = true
    mocks.route.path = '/admin/pool'
    const admin = mountPage()
    await flushPromises()
    expect(mocks.accounts).toHaveBeenLastCalledWith(expect.anything(), 'admin', expect.anything())
    expect(mocks.groups).toHaveBeenCalledTimes(1)
    admin.unmount()
    mocks.route.path = '/pool'
    const personal = mountPage()
    await flushPromises()
    expect(mocks.accounts).toHaveBeenLastCalledWith(expect.anything(), 'user', expect.anything())
    personal.unmount()
  })
})
