import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { bootstrapPoolShareApp } from '../poolShareApp'

vi.mock('@/stores/auth', () => ({ useAuthStore: () => { throw new Error('A shared document must not restore console auth') } }))
const fetchMock = vi.fn()
let mounted: Awaited<ReturnType<typeof bootstrapPoolShareApp>> | undefined

beforeEach(() => {
  document.body.innerHTML = '<div id="app"></div>'
  window.history.replaceState({}, '', '/share/pool?share_token=pool_PUBLIC_TEST&token=FOREIGN_JWT&tab=usage&theme=dark&lang=zh-CN')
  localStorage.setItem('auth_token', 'LOCAL_ADMIN_CREDENTIAL_MUST_NOT_BE_SENT')
  fetchMock.mockReset().mockResolvedValue({ ok: true, status: 200, json: async () => ({ code: 0, data: { items: [], total: 0, page: 1, page_size: 20 } }) })
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  mounted?.app.unmount()
  mounted?.router.options.history.destroy()
  mounted = undefined
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  localStorage.clear()
  document.documentElement.classList.remove('dark')
  window.history.replaceState({}, '', '/')
})

describe('read-only share document bootstrap', () => {
  it('loads only scoped usage even when local admin and foreign site login tokens are present', async () => {
    mounted = await bootstrapPoolShareApp()
    await flushPromises()
    expect(document.documentElement.lang).toBe('zh')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('/pool-share/usage?'), expect.objectContaining({
      credentials: 'omit', headers: { 'X-Pool-Share-Token': 'pool_PUBLIC_TEST' },
    }))
    expect(document.body.textContent).not.toContain('admin.pool.sharePool')
    await mounted.router.push('/admin/accounts')
    await flushPromises()
    expect(mounted.router.currentRoute.value.path).toBe('/share/pool')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('works when third-party storage access is denied', async () => {
    vi.spyOn(window, 'localStorage', 'get').mockImplementation(() => { throw new DOMException('Blocked third-party storage', 'SecurityError') })
    mounted = await bootstrapPoolShareApp()
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(document.body.querySelector('[role="tablist"]')).not.toBeNull()
  })

  it('does not fetch data for the host site login token without a share credential', async () => {
    window.history.replaceState({}, '', '/share/pool?token=FOREIGN_JWT&ui_mode=embedded')
    mounted = await bootstrapPoolShareApp()
    await flushPromises()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(document.body.querySelector('[role="alert"]')).not.toBeNull()
  })
})
