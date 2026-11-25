import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ReloginAccount } from '@/api/relogin'
import AccountUsageCell from '@/components/account/AccountUsageCell.vue'
import UsageProgressBar from '@/components/account/UsageProgressBar.vue'
import ReloginAccounts from './ReloginAccounts.vue'

const { getUsage } = vi.hoisted(() => ({ getUsage: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { getUsage } } }))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<object>(),
  useI18n: () => ({ t: (key: string) => key })
}))

function account(id: number): ReloginAccount {
  return {
    id, email: 'owner@example.test', name: 'OAuth', platform: 'openai', type: 'oauth',
    plan_type: 'pro', has_imported: true, credentials: { plan_type: 'pro' },
    proxy_id: null, concurrency: 3, priority: 1, status: 'active', schedulable: true,
    error_message: '', last_used_at: null, expires_at: null, auto_pause_on_expired: true,
    created_at: '2026-11-25T00:00:00Z', updated_at: '2026-11-25T00:00:00Z',
    rate_limited_at: null, rate_limit_reset_at: null, overload_until: null,
    temp_unschedulable_until: null, temp_unschedulable_reason: null,
    session_window_start: null, session_window_end: null, session_window_status: null
  }
}

function setup(id: number) {
  return mount(ReloginAccounts, {
    props: { accounts: [account(id)], runs: [], busy: false, usageRefreshToken: 0 },
    global: { stubs: { OpenAIQuotaResetCell: { template: '<div><slot name="pre-actions" /></div>' } } }
  })
}

beforeEach(() => vi.clearAllMocks())

describe('失效重登额度展示', () => {
  it('复用账号管理的 5h、7d 额度与重置时间，并响应页面刷新', async () => {
    getUsage.mockResolvedValue({
      five_hour: { utilization: 23, resets_at: '2026-11-25T05:00:00Z' },
      seven_day: { utilization: 67, resets_at: '2026-12-01T00:00:00Z' }
    })
    const wrapper = setup(98601)
    await flushPromises()
    expect(getUsage).toHaveBeenCalledWith(98601)
    expect(wrapper.getComponent(AccountUsageCell).props('account').id).toBe(98601)
    const bars = wrapper.findAllComponents(UsageProgressBar)
    expect(bars.map(bar => bar.props('label'))).toEqual(['5h', '7d'])
    expect(bars.map(bar => bar.props('utilization'))).toEqual([23, 67])
    expect(bars[1].props('resetsAt')).toBe('2026-12-01T00:00:00Z')
    expect(wrapper.text()).toContain('23%')
    expect(wrapper.text()).toContain('67%')

    getUsage.mockResolvedValue({ five_hour: { utilization: 31 }, seven_day: { utilization: 72 } })
    await wrapper.setProps({ usageRefreshToken: 1 })
    await flushPromises()
    expect(getUsage).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('31%')
    expect(wrapper.text()).toContain('72%')
    wrapper.getComponent(AccountUsageCell).vm.$emit('account-updated', account(98601))
    expect(wrapper.emitted('refresh')).toHaveLength(1)
    wrapper.unmount()
  })

  it('额度未知时显示未知，不用 0% 代替', async () => {
    getUsage.mockResolvedValue({
      five_hour: { utilization: 0, utilization_unknown: true },
      seven_day: { utilization: 0, utilization_unknown: true },
      error_code: 'not_observed'
    })
    const wrapper = setup(98602)
    await flushPromises()
    const bars = wrapper.findAllComponents(UsageProgressBar)
    expect(bars).toHaveLength(2)
    expect(bars.every(bar => bar.props('utilizationUnknown'))).toBe(true)
    expect(wrapper.text()).not.toContain('0%')
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    wrapper.unmount()
  })
})
