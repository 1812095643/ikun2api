import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import UsageServiceTierBadge from './UsageServiceTierBadge.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('UsageServiceTierBadge', () => {
  it.each([['priority', 2, 'Fast ×2'], ['ultrafast', 6, 'Ultrafast ×6'], ['ultrafast', 8, 'Ultrafast ×8'], ['fast', 2.5, 'Fast ×2.5'], ['priority', 1, 'Fast ×1']])('shows the saved %s price multiplier', (tier, rate, text) => {
    const wrapper = mount(UsageServiceTierBadge, { props: { row: { service_tier: String(tier), service_tier_multiplier: Number(rate) } } })
    expect(wrapper.text()).toBe(text)
    expect(wrapper.classes()).toContain('text-[10px]')
    if (tier === 'ultrafast') expect(wrapper.classes()).toContain('ultrafast-billing-marker')
  })
  it('does not invent a price for historical usage without a snapshot', () => {
    const wrapper = mount(UsageServiceTierBadge, { props: { row: { model: 'gpt-6.1-sol', service_tier: 'ultrafast' } } })
    expect(wrapper.text()).toBe('Ultrafast')
  })
  it.each(['', 'default', 'standard', 'flex'])('leaves %s usage unchanged', tier => {
    const wrapper = mount(UsageServiceTierBadge, { props: { row: { service_tier: tier } } })
    expect(wrapper.find('[data-testid="service-tier-billing-marker"]').exists()).toBe(false)
  })
})
