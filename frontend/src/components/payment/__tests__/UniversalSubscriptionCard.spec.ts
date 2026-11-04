import { mount } from '@vue/test-utils'
import { describe, it, expect } from 'vitest'
import UniversalSubscriptionCard from '../UniversalSubscriptionCard.vue'
import type { UniversalSubscription } from '@/api/universalSubscriptions'
describe('通用订阅卡片', () => {
 it('shows shared quota and a strict no-wallet-fallback rule', () => {
  const subscription = { id: 1, name: '通用月卡', status: 'active', total_credit_usd: 100, remaining_usd: 74, total_used_usd: 24.8, daily_used_usd: 3.2, daily_limit_usd: 10, weekly_used_usd: 24.8, weekly_limit_usd: 40, monthly_used_usd: 24.8, monthly_limit_usd: 100, held_usd: 1.2, starts_at: '2026-11-01T00:00:00Z', expires_at: '2026-12-01T00:00:00Z' } as UniversalSubscription
  const wrapper = mount(UniversalSubscriptionCard, { props: { subscription } }); expect(wrapper.text()).toContain('74'); expect(wrapper.text()).toContain('所有已授权分组与订阅 Key 共用'); expect(wrapper.text()).toContain('不扣除普通余额'); expect(wrapper.findAll('[role="progressbar"]')).toHaveLength(3)
 })
})
