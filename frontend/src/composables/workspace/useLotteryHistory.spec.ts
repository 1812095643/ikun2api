import { defineComponent, reactive } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useLotteryHistory } from './useLotteryHistory'

const participations = vi.hoisted(() => vi.fn())
vi.mock('@/api/lottery', () => ({ lotteryAPI: { participations } }))
vi.mock('@/api/workspace', () => ({ workspaceError: () => '暂时无法读取' }))

describe('抽奖参与记录查询', () => {
  beforeEach(() => {
    participations.mockReset()
    participations.mockResolvedValue({ items: [{ id: 3, result: 'lost' }], total: 22 })
  })
  function setup(manage = false) {
    const options = reactive<{ manage: boolean; lotteryId?: number; revision: number }>({ manage, revision: 0 })
    let state!: ReturnType<typeof useLotteryHistory>
    const wrapper = mount(defineComponent({ setup() { state = useLotteryHistory(options); return () => null } }))
    return { state, wrapper, options }
  }
  it('个人记录不提交管理员用户筛选并保留未中奖记录', async () => {
    const { state, wrapper } = setup()
    await flushPromises()
    state.search.value = 'another@example.invalid'
    state.apply()
    await flushPromises()
    expect(participations.mock.lastCall?.[0]).toBe(false)
    expect(participations.mock.lastCall?.[1].search).toBeUndefined()
    expect(state.data.value?.items[0].result).toBe('lost')
    wrapper.unmount()
  })
  it('管理员按活动和用户筛选，结果切换回到第一页', async () => {
    const { state, wrapper, options } = setup(true)
    await flushPromises()
    state.activity.value = '周末'
    state.search.value = 'owner@example.invalid'
    state.apply()
    await flushPromises()
    state.changePage(1)
    await flushPromises()
    state.result.value = 'credited'
    await flushPromises()
    expect(participations.mock.lastCall?.[0]).toBe(true)
    expect(participations.mock.lastCall?.[1]).toMatchObject({ page: 1, activity: '周末', search: 'owner@example.invalid', result: 'credited' })
    options.lotteryId = 9
    await flushPromises()
    expect(participations.mock.lastCall?.[1].lottery_id).toBe(9)
    wrapper.unmount()
  })
  it('切换筛选后忽略已取消请求的旧响应', async () => {
    let finish!: (value: unknown) => void
    participations.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const { state, wrapper } = setup()
    state.result.value = 'won'
    await flushPromises()
    finish({ items: [{ id: 1, result: 'pending' }], total: 1 })
    await flushPromises()
    expect(state.data.value?.items[0].id).toBe(3)
    wrapper.unmount()
  })
})
