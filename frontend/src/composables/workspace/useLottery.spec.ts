import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useLottery } from './useLottery'

const api = vi.hoisted(() => ({ list: vi.fn(), wins: vi.fn(), win: vi.fn(), claim: vi.fn() }))
const refreshUser = vi.hoisted(() => vi.fn())
vi.mock('@/api/lottery', () => ({ lotteryAPI: api }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ refreshUser }) }))
vi.mock('@/api/workspace', () => ({ workspaceError: () => '暂时无法读取' }))

describe('中奖后核验与自动入账', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.list.mockResolvedValue({ items: [], total: 0, server_time: new Date().toISOString() })
    api.wins.mockImplementation(async () => ({ items: [{ id: 12, amount: '3', claimed_at: null }], total: 1, total_amount: '3' }))
    api.win.mockResolvedValue({ id: 12, amount: '3', claimed_at: null })
    api.claim.mockResolvedValue({ id: 12, amount: '3', claimed_at: new Date().toISOString(), redeem_id: 9 })
    refreshUser.mockResolvedValue({ balance: 3 })
  })
  function setup() {
    let state!: ReturnType<typeof useLottery>
    const wrapper = mount(defineComponent({ setup() { state = useLottery(); return () => null } }))
    return { state, wrapper }
  }
  it('先查询核验，自动入账仅传记录编号，旧响应不会重复发放', async () => {
    const { state, wrapper } = setup()
    await flushPromises()
    expect(api.win).toHaveBeenCalledWith(12)
    expect(api.claim).toHaveBeenCalledWith(12)
    expect(api.win.mock.invocationCallOrder[0]).toBeLessThan(api.claim.mock.invocationCallOrder[0])
    expect(state.wins.value[0].redeem_id).toBe(9)
    expect(refreshUser).toHaveBeenCalledOnce()
    await state.load(true)
    expect(api.claim).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
  it('核验未通过时不发充值请求，保留记录供用户重试', async () => {
    api.win.mockRejectedValue(new Error('signature mismatch'))
    const { state, wrapper } = setup()
    await flushPromises()
    expect(api.claim).not.toHaveBeenCalled()
    expect(state.wins.value[0].claimed_at).toBeNull()
    expect(state.feedback.value).toContain('暂未入账')
    wrapper.unmount()
  })
})
