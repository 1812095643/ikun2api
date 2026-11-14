import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useUpstreamBalanceDisplay } from '../useUpstreamBalanceDisplay'
import { defaultBalanceDisplay } from '@/api/upstreamBalances'
const api = vi.hoisted(() => ({ saveDisplay: vi.fn() }))
const showError = vi.hoisted(() => vi.fn())
vi.mock('@/api/upstreamBalances', async original => ({ ...(await original<typeof import('@/api/upstreamBalances')>()), upstreamBalancesAPI: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError }) }))

beforeEach(() => { vi.resetAllMocks(); api.saveDisplay.mockImplementation(async value => value) })
describe('上游显示偏好保存', () => {
  it('没有读到服务器配置前不允许默认值覆盖设置', () => {
    const state = useUpstreamBalanceDisplay()
    state.updateDisplay({ hidden: ['a'] })
    expect(api.saveDisplay).not.toHaveBeenCalled()
  })
  it('连续修改串行合并保存，轮询不覆盖正在编辑的排序', async () => {
    let resolve!: (value: unknown) => void
    api.saveDisplay.mockImplementationOnce(() => new Promise(r => { resolve = r }))
    const state = useUpstreamBalanceDisplay()
    state.initializeDisplay(defaultBalanceDisplay())
    state.setHidden(['a'], true)
    state.updateDisplay({ order: ['c', 'b', 'a'] })
    state.updateDisplay({ hidden_columns: ['monitor'] })
    state.initializeDisplay(defaultBalanceDisplay())
    expect(api.saveDisplay).toHaveBeenCalledTimes(1)
    resolve({ ...defaultBalanceDisplay(), hidden: ['a'] })
    await flushPromises()
    expect(api.saveDisplay).toHaveBeenCalledTimes(2)
    expect(api.saveDisplay.mock.calls[1][0]).toMatchObject({ hidden: ['a'], order: ['c', 'b', 'a'], hidden_columns: ['monitor'] })
    expect(state.displaySaving.value).toBe(false)
  })
  it('隐藏可恢复，保存失败恢复最后确认的配置并提示', async () => {
    const state = useUpstreamBalanceDisplay()
    state.initializeDisplay({ ...defaultBalanceDisplay(), hidden: ['a'] })
    state.setHidden(['a'], false)
    await flushPromises()
    expect(state.display.value.hidden).toEqual([])
    api.saveDisplay.mockRejectedValue(new Error('network'))
    state.updateDisplay({ order: ['a'] })
    await flushPromises()
    expect(state.display.value.order).toEqual([])
    expect(state.displayError.value).toContain('保存失败')
    expect(showError).toHaveBeenCalledOnce()
  })
})
