import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defaultBalanceDisplay, type UpstreamBalance } from '@/api/upstreamBalances'
import { useUpstreamBalances } from '../useUpstreamBalances'
const api = vi.hoisted(() => ({ list: vi.fn(), refresh: vi.fn(), save: vi.fn(), saveDisplay: vi.fn() }))
const app = vi.hoisted(() => ({ showSuccess: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/upstreamBalances', async original => ({ ...(await original<typeof import('@/api/upstreamBalances')>()), upstreamBalancesAPI: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => app }))
function setup() {
  let state!: ReturnType<typeof useUpstreamBalances>
  const wrapper = mount(defineComponent({ setup() { state = useUpstreamBalances(); return () => null } }))
  return { state, wrapper }
}
const row = (id: string) => ({ id, name: id, url: `https://${id}.example`, accounts: [], snapshot: { balance: 5 }, alert: { low: false }, config: { monitor_enabled: false } }) as UpstreamBalance
beforeEach(() => {
  vi.resetAllMocks()
  api.list.mockResolvedValue({ items: [row('a'), row('b'), row('c')], display: { ...defaultBalanceDisplay(), hidden: ['b'] } })
  api.saveDisplay.mockImplementation(async d => d)
  api.refresh.mockResolvedValue({ queued: 1 })
})
describe('上游列表交互', () => {
  it('默认隐藏已隐藏上游，切换后可以恢复并保留其他设置', async () => {
    const { state, wrapper } = setup(); await flushPromises()
    expect(state.visible.value.map(row => row.id)).toEqual(['a', 'c'])
    state.visibility.value = 'hidden'; await flushPromises()
    expect(state.visible.value.map(row => row.id)).toEqual(['b'])
    state.setHidden(['b'], false); await flushPromises()
    expect(state.hiddenCount.value).toBe(0)
    expect(state.visible.value).toEqual([])
    wrapper.unmount()
  })
  it('拖动当前列表时保留隐藏项的位置，轮询不恢复旧顺序', async () => {
    const { state, wrapper } = setup(); await flushPromises()
    state.reorder(['c', 'a']); await flushPromises()
    expect(state.display.value.order).toEqual(['c', 'b', 'a'])
    await state.load(false)
    expect(state.visible.value.map(row => row.id)).toEqual(['c', 'a'])
    wrapper.unmount()
  })
  it('查询所选提交精确 ID，空选择不会扩大成全部查询', async () => {
    const { state, wrapper } = setup(); await flushPromises()
    await state.refresh(undefined, ['c'])
    expect(api.refresh).toHaveBeenCalledWith(undefined, ['c'])
    await state.refresh(undefined, [])
    expect(api.refresh).toHaveBeenCalledOnce()
    wrapper.unmount()
  })
  it('刷新失败保留已显示的数据和显隐设置', async () => {
    const { state, wrapper } = setup(); await flushPromises()
    api.list.mockRejectedValue(new Error('network'))
    await state.load(false)
    expect(state.items.value).toHaveLength(3)
    expect(state.visible.value.map(row => row.id)).toEqual(['a', 'c'])
    expect(state.error.value).toContain('加载失败')
    wrapper.unmount()
  })
})
