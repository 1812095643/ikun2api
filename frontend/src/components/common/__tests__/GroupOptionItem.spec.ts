import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import GroupOptionItem from '../GroupOptionItem.vue'
import GroupBadge from '../GroupBadge.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }),
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ cachedPublicSettings: null }),
}))

describe('GroupOptionItem description layout', () => {
  it('applies multiline and overflow-safe text styles', () => {
    const description = 'First section\nvery-long-unbroken-description-value-that-must-not-overflow'
    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'Example group',
        platform: 'openai',
        description,
      },
      global: {
        stubs: {
          GroupBadge: true,
        },
      },
    })

    const descriptionElement = wrapper
      .findAll('span')
      .find((element) => element.text() === description)

    expect(descriptionElement).toBeDefined()
    expect(descriptionElement?.classes()).toContain('whitespace-pre-line')
    expect(descriptionElement?.classes()).toContain('[overflow-wrap:anywhere]')
    expect(descriptionElement?.classes()).toContain('line-clamp-3')
    expect(wrapper.find('[title]').attributes('title')).toBe(description)
  })
})


describe('price change display boundary', () => {
  it('shows price changes and NEW in a group selection option', () => {
    const w = mount(GroupOptionItem, {
      props: { name: 'Group', platform: 'anthropic', rateMultiplier: 1, previousRateMultiplier: 0.4,
        rateMultiplierChangedAt: new Date(Date.now() - 86400000).toISOString(), createdAt: new Date().toISOString() },
      global: { stubs: { PlatformIcon: true } }
    })
    expect(w.get('[data-price-change]').text()).toBe('↑ 0.6')
    expect(w.find('[data-new-group]').exists()).toBe(true)
    w.unmount()
  })
  it('does not expose change or NEW badges in normal table/selected group badges', () => {
    const w = mount(GroupBadge, { props: { name: 'Group', platform: 'anthropic', rateMultiplier: 1 }, global: { stubs: { PlatformIcon: true } } })
    expect(w.text()).toContain('1x')
    expect(w.find('[data-price-change]').exists()).toBe(false)
    expect(w.find('[data-new-group]').exists()).toBe(false)
    w.unmount()
  })
})
