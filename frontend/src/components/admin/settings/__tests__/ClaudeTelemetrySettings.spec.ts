import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ClaudeTelemetrySettings from '../ClaudeTelemetrySettings.vue'

const mocks = vi.hoisted(() => ({ get: vi.fn(), update: vi.fn() }))
vi.mock('@/api/admin/claudeTelemetry', () => ({ getClaudeTelemetrySettings: mocks.get, updateClaudeTelemetrySettings: mocks.update }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const defaults = { enabled: true, flush_interval_seconds: 10, min_send_gap_seconds: 2, timeout_seconds: 10 }

beforeEach(() => {
  vi.clearAllMocks()
  mocks.get.mockResolvedValue({ ...defaults })
  mocks.update.mockImplementation(async value => value)
})

describe('Claude telemetry settings', () => {
  it('loads the stored switch and saves parameters without submitting the parent form', async () => {
    const wrapper = mount(ClaudeTelemetrySettings)
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('true')
    await wrapper.get('[role="switch"]').trigger('click')
    await wrapper.get('[data-testid="flush_interval_seconds"]').setValue(25)
    await wrapper.get('[data-testid="save-telemetry"]').trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith({ ...defaults, enabled: false, flush_interval_seconds: 25 })
    expect(wrapper.get('[data-testid="save-telemetry"]').attributes('type')).toBe('button')
  })

  it('blocks saving an unreadable configuration and permits retry', async () => {
    mocks.get.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mount(ClaudeTelemetrySettings)
    await flushPromises()
    expect(wrapper.get('[data-testid="save-telemetry"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[role="alert"]').text()).toContain('offline')
    await wrapper.get('.btn-secondary').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="save-telemetry"]').attributes('disabled')).toBeUndefined()
  })

  it('rejects unsafe intervals and fractional values before sending', async () => {
    const wrapper = mount(ClaudeTelemetrySettings)
    await flushPromises()
    for (const value of [0, 1, 2.5, 61]) {
      await wrapper.get('[data-testid="min_send_gap_seconds"]').setValue(value)
      await wrapper.get('[data-testid="save-telemetry"]').trigger('click')
    }
    expect(mocks.update).not.toHaveBeenCalled()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
  })

  it('retains values and shows an error after a failed save', async () => {
    mocks.update.mockRejectedValueOnce(new Error('save failed'))
    const wrapper = mount(ClaudeTelemetrySettings)
    await flushPromises()
    await wrapper.get('[data-testid="timeout_seconds"]').setValue(20)
    await wrapper.get('[data-testid="save-telemetry"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('save failed')
    expect((wrapper.get('[data-testid="timeout_seconds"]').element as HTMLInputElement).value).toBe('20')
  })
})
