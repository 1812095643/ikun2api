import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OpsSettingsDialog from '../OpsSettingsDialog.vue'

const { api, showError } = vi.hoisted(() => ({
  api: {
    getAlertRuntimeSettings: vi.fn(), getEmailNotificationConfig: vi.fn(),
    getAdvancedSettings: vi.fn(), getMetricThresholds: vi.fn(),
    updateAlertRuntimeSettings: vi.fn(), updateEmailNotificationConfig: vi.fn(),
    updateAdvancedSettings: vi.fn(), updateMetricThresholds: vi.fn(),
  },
  showError: vi.fn(),
}))
vi.mock('@/api/admin/ops', () => ({ opsAPI: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('Ops client restriction error filter', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.getAlertRuntimeSettings.mockResolvedValue({ evaluation_interval_seconds: 30 })
    api.getEmailNotificationConfig.mockResolvedValue({
      alert: { enabled: false, recipients: [] }, report: { enabled: false, recipients: [] },
    })
    api.getMetricThresholds.mockResolvedValue({})
    api.updateAlertRuntimeSettings.mockResolvedValue({})
    api.updateEmailNotificationConfig.mockResolvedValue({})
    api.updateMetricThresholds.mockResolvedValue({})
  })

  it.each([undefined, false, true])('loads, toggles, saves and reopens (stored: %s)', async (initial) => {
    let stored = {
      data_retention: { error_log_retention_days: 30, minute_metrics_retention_days: 30, hourly_metrics_retention_days: 30, cleanup_enabled: false },
      aggregation: { aggregation_enabled: false },
      ignore_count_tokens_errors: true,
      ignore_context_canceled: true,
      ignore_no_available_accounts: false,
      ignore_insufficient_balance_errors: true,
      ignore_client_restriction_errors: initial,
      auto_refresh_enabled: false,
      display_alert_events: true,
      display_openai_token_stats: false,
    }
    api.getAdvancedSettings.mockImplementation(async () => structuredClone(stored))
    api.updateAdvancedSettings.mockImplementation(async (value) => { stored = JSON.parse(JSON.stringify(value)); return stored })
    const wrapper = mount(OpsSettingsDialog, {
      props: { show: false },
      global: { stubs: { BaseDialog: { template: '<section><slot /><slot name="footer" /></section>' }, Select: true } },
    })
    await wrapper.setProps({ show: true })
    await flushPromises()
    const toggle = wrapper.get('[data-testid="ops-ignore-client-restriction"]')
    expect(toggle.attributes('aria-checked')).toBe(String(initial ?? false))
    await toggle.trigger('click')
    const expected = !(initial ?? false)
    expect(toggle.attributes('aria-checked')).toBe(String(expected))
    const save = wrapper.findAll('button').find(button => button.text() === 'common.save')
    expect(save).toBeDefined()
    await save!.trigger('click')
    await flushPromises()
    expect(showError).not.toHaveBeenCalled()
    expect(api.updateAdvancedSettings).toHaveBeenCalledWith(expect.objectContaining({
      ignore_client_restriction_errors: expected,
      ignore_count_tokens_errors: true,
      ignore_no_available_accounts: false,
      ignore_insufficient_balance_errors: true,
    }))
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.get('[data-testid="ops-ignore-client-restriction"]').attributes('aria-checked')).toBe(String(expected))
    wrapper.unmount()
  })
})
