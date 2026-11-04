import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getPoolOverview, listPoolAccounts, listPoolAccountRequests, listPoolUsage } from '../pool'

const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { get: mocks.get } }))

beforeEach(() => {
  vi.clearAllMocks()
  mocks.get.mockResolvedValue({ data: { items: [], total: 0 } })
})

describe('pool API scope', () => {
  it.each(['user', 'admin'] as const)('keeps %s requests in their own namespace and forwards cancellation', async scope => {
    const signal = new AbortController().signal
    const base = scope === 'admin' ? '/admin/pool' : '/pool'
    await getPoolOverview(scope, { signal })
    expect(mocks.get).toHaveBeenLastCalledWith(base + '/overview', { signal })
    await listPoolAccounts({ page: 2 }, scope, { signal })
    expect(mocks.get).toHaveBeenLastCalledWith(base + '/accounts', { params: { page: 2 }, signal })
    await listPoolAccountRequests(9, { page: 1 }, scope, { signal })
    expect(mocks.get).toHaveBeenLastCalledWith(base + '/accounts/9/requests', { params: { page: 1 }, signal })
    await listPoolUsage({ account_id: 9 }, scope, { signal })
    expect(mocks.get).toHaveBeenLastCalledWith(base + '/usage', { params: { account_id: 9 }, signal })
  })
})
