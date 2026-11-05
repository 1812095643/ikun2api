import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { listSharedPoolAccounts, listSharedPoolUsage, PoolShareUnavailableError } from '../poolShare'

const fetchMock = vi.fn()
beforeEach(() => { vi.stubGlobal('fetch', fetchMock); fetchMock.mockReset() })
afterEach(() => vi.unstubAllGlobals())

describe('scoped pool share transport', () => {
  it.each([['accounts', listSharedPoolAccounts], ['usage', listSharedPoolUsage]] as const)('reads %s without login credentials or URL tokens', async (resource, list) => {
    const data = { items: [], page: 2, total: 0 }
    fetchMock.mockResolvedValue({ ok: true, status: 200, json: async () => ({ code: 0, data }) })
    const signal = new AbortController().signal
    expect(await list('pool_TEST', 2, signal)).toEqual(data)
    const [url, options] = fetchMock.mock.calls[0]
    expect(url).toBe(`/api/v1/pool-share/${resource}?page=2&page_size=20`)
    expect(url).not.toContain('pool_TEST')
    expect(options).toEqual({ signal, credentials: 'omit', referrerPolicy: 'no-referrer', cache: 'no-store', redirect: 'error', headers: { 'X-Pool-Share-Token': 'pool_TEST' } })
  })
  it.each([401, 403])('treats %s as an unavailable link', async status => {
    fetchMock.mockResolvedValue({ ok: false, status })
    await expect(listSharedPoolUsage('pool_TEST', 1)).rejects.toBeInstanceOf(PoolShareUnavailableError)
  })
  it('does not request with an empty token', async () => {
    await expect(listSharedPoolUsage('', 1)).rejects.toBeInstanceOf(PoolShareUnavailableError)
    expect(fetchMock).not.toHaveBeenCalled()
  })
  it.each([429, 503])('keeps %s recoverable without marking the token invalid', async status => {
    fetchMock.mockResolvedValue({ ok: false, status })
    await expect(listSharedPoolUsage('pool_TEST', 1)).rejects.not.toBeInstanceOf(PoolShareUnavailableError)
  })
  it('rejects malformed data rather than displaying an empty list', async () => {
    fetchMock.mockResolvedValue({ ok: true, status: 200, json: async () => ({ code: 0, data: null }) })
    await expect(listSharedPoolUsage('pool_TEST', 1)).rejects.toThrow('Invalid shared pool response')
  })
})
