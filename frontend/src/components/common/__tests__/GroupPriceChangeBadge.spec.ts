import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh'
import GroupPriceChangeBadge from '../GroupPriceChangeBadge.vue'

const now = new Date('2026-11-06T08:00:00Z')
const changed = '2026-11-05T08:00:00Z'
const wrappers: ReturnType<typeof mount>[] = []
function render(props = {}) {
  const wrapper = mount(GroupPriceChangeBadge, {
    props: { rateMultiplier: 1, previousRateMultiplier: 0.4, rateMultiplierChangedAt: changed, ...props },
    global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh } })] },
  })
  wrappers.push(wrapper)
  return wrapper
}
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(now) })
afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); vi.useRealTimers() })

describe('group price changes', () => {
  it('shows the last increase with an exact decimal difference and previous/current rates', () => {
    const w = render()
    expect(w.get('[data-price-change]').text()).toBe('↑ 0.6')
    expect(w.get('[data-price-change]').attributes('title')).toContain('0.4x → 1x')
  })
  it('shows decreases without floating-point noise', () => {
    const w = render({ rateMultiplier: 0.14, previousRateMultiplier: 0.15 })
    expect(w.get('[data-price-change]').text()).toBe('↓ 0.01')
  })
  it.each([
    { previousRateMultiplier: undefined },
    { previousRateMultiplier: 1 },
    { previousRateMultiplier: Number.NaN },
    { rateMultiplier: Number.POSITIVE_INFINITY },
    { rateMultiplierChangedAt: 'invalid' },
    { rateMultiplierChangedAt: '2027-01-01T00:00:00Z' },
    { userRateMultiplier: 0.5 },
    { userRateMultiplier: 1 },
  ])('suppresses absent, unchanged, invalid or personally overridden prices: %s', props => {
    expect(render(props).find('[data-price-change]').exists()).toBe(false)
  })
  it('keeps the last actual change visible after unrelated updates or a week has passed', () => {
    expect(render({ rateMultiplierChangedAt: '2026-10-01T08:00:00Z' }).get('[data-price-change]').text()).toBe('↑ 0.6')
  })
  it('shows NEW for seven days and expires while the page remains open', async () => {
    const w = render({ previousRateMultiplier: null, createdAt: '2026-10-30T08:00:30Z' })
    expect(w.find('[data-new-group]').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(w.find('[data-new-group]').exists()).toBe(false)
  })
  it.each(['2026-10-30T08:00:00Z', '2026-11-07T08:00:00Z', 'invalid'])('does not label old or invalid timestamps as new: %s', createdAt => {
    expect(render({ createdAt }).find('[data-new-group]').exists()).toBe(false)
  })
})
