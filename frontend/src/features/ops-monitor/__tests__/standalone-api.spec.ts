import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { monitorAPI } from '../../../../monitor-standalone/api'

describe('独立监控页面访问', () => {
  beforeEach(() => { localStorage.clear(); vi.restoreAllMocks() })
  afterEach(() => { vi.unstubAllGlobals(); localStorage.clear() })
  it('requires the same-origin administrator session', async () => {
    await expect(monitorAPI.summary()).rejects.toThrow('请先登录')
  })
  it('keeps the session in the Authorization header and unwraps the result', async () => {
    localStorage.setItem('auth_token', 'synthetic-token')
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ code: 0, data: { build: 'test' } }) })
    vi.stubGlobal('fetch', fetchMock)
    await expect(monitorAPI.summary()).resolves.toEqual({ build: 'test' })
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/admin/ops/monitor/summary')
    expect(fetchMock.mock.calls[0][0]).not.toContain('synthetic-token')
    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe('Bearer synthetic-token')
  })
  it('does not show a false healthy state on permission failures', async () => {
    localStorage.setItem('auth_token', 'synthetic-token')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 403, json: async () => ({ message: 'forbidden' }) }))
    await expect(monitorAPI.summary()).rejects.toThrow('无权访问')
  })
})
