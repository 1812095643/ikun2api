import { defineComponent } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, afterEach, describe, it, expect, vi } from 'vitest'
import { useChannelStatusCards } from '../useChannelStatusCards'

const { getSnapshot, getMatrix } = vi.hoisted(() => ({ getSnapshot: vi.fn(), getMatrix: vi.fn() }))
vi.mock('@/api/channelMonitorV2', () => ({ getSnapshot, getMatrix }))
vi.mock('@/api/groups', () => ({ default: { getAvailable: vi.fn().mockResolvedValue([]), getUserGroupRates: vi.fn().mockResolvedValue({}) } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
let state: ReturnType<typeof useChannelStatusCards>
let wrapper: ReturnType<typeof mount>
const result = (name: string) => ({ config: { refresh_interval_seconds: 60 }, coverage: {}, items: [{ group_id: 1, platform: name }] })
function mountFeature() { wrapper = mount(defineComponent({ setup: () => { state = useChannelStatusCards(); return () => null } })) }
beforeEach(() => { vi.useFakeTimers(); getSnapshot.mockReset(); getMatrix.mockReset() })
afterEach(() => { wrapper?.unmount(); vi.useRealTimers() })

describe('channel refresh isolation', () => {
  it('ignores older responses after changing the time range', async () => {
    let releaseSnapshot!: (value: unknown) => void
    let releaseMatrix!: (value: unknown) => void
    getSnapshot.mockReturnValueOnce(new Promise(resolve => { releaseSnapshot = resolve })).mockResolvedValue(result('new'))
    getMatrix.mockReturnValueOnce(new Promise(resolve => { releaseMatrix = resolve })).mockResolvedValue(result('new'))
    mountFeature()
    state.setRange('7d')
    await flushPromises()
    expect(state.matrix.value?.items[0].platform).toBe('new')
    releaseSnapshot(result('old')); releaseMatrix(result('old'))
    await flushPromises()
    expect(state.matrix.value?.items[0].platform).toBe('new')
    expect(getMatrix.mock.calls[0][3].aborted).toBe(true)
  })
  it('retains the last snapshot and exposes a network error on refresh failure', async () => {
    getSnapshot.mockResolvedValue(result('retained')); getMatrix.mockResolvedValue(result('retained'))
    mountFeature(); await flushPromises()
    getSnapshot.mockRejectedValueOnce(new Error('offline'))
    await state.reload()
    expect(state.matrix.value?.items[0].platform).toBe('retained')
    expect(state.error.value).toBeTruthy()
    expect(state.countdown.value).toBe(60)
  })
})
