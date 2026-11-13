import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ModelCard from './ModelCard.vue'
import type { ModelEntry } from '@/composables/workspace/useModelSquare'
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

function entry(name: string, platform: string, withOriginal = true): ModelEntry {
  return {
    name, platform,
    offers: [{
      group: { id: 1, rate_multiplier: 2, user_rate_multiplier: 0.5 } as ModelPlazaGroup,
      model: {
        name, platform,
        pricing: {
          billing_mode: 'token', input_price: 5e-6, output_price: 20e-6,
          cache_read_price: 1e-6, cache_write_price: 6e-6, cache_write_1h_price: 12e-6
        } as NonNullable<PlazaModel['pricing']>,
        official_pricing: withOriginal
          ? { input_price: 4e-6, output_price: 20e-6, cache_read_price: 0.4e-6, cache_write_price: 5e-6, cache_write_1h_price: 8e-6 }
          : null
      }
    }]
  }
}

describe('模型卡片价格来源', () => {
  it('原价没有 1h 缓存创建项时不拿渠道价补成官方价', () => {
    const value = entry('gpt-5.4', 'openai')
    value.offers[0].model.official_pricing!.cache_write_1h_price = null
    const wrapper = mount(ModelCard, { props: { entry: value } })
    const row = wrapper.find('[data-price-field="cache_write_1h_price"]')
    expect(row.exists()).toBe(false)
    expect(value.offers[0].model.pricing!.cache_write_1h_price).toBe(12e-6)
    wrapper.unmount()
  })

  it.each([['claude-sonnet-5-5', 'anthropic'], ['gpt-5.5', 'openai']])(
    '%s 五项实付显示官方原价乘倍率，包含缓存费用', (name, platform) => {
      const wrapper = mount(ModelCard, { props: { entry: entry(name, platform) } })
      expect(wrapper.find('.model-price-head').text()).toContain('实付价格官方价格')
      for (const [field, paid, official] of [
        ['input_price', '$2.00', '$4.00'],
        ['output_price', '$10.00', '$20.00'],
        ['cache_read_price', '$0.20', '$0.40'],
        ['cache_write_price', '$2.50', '$5.00'],
        ['cache_write_1h_price', '$4.00', '$8.00']
      ]) {
        const row = wrapper.find(`[data-price-field="${field}"]`)
        expect(row.find('strong').text()).toBe(paid)
        expect(row.find('.model-original-price').text()).toBe(official)
      }
      expect(wrapper.find('.price-caption').text()).toBe('· 最低实付价')
      expect(wrapper.find('.model-footer button').text()).toContain('计价详情')
      wrapper.unmount()
    }
  )

  it.each([['gemini-2.5-pro', 'gemini', true], ['deepseek-chat', 'deepseek', true], ['gpt-unlisted', 'openai', false]] as const)(
    '%s 官方列显示不乘倍率的渠道价，实付保持原逻辑', (name, platform, withOriginal) => {
      const wrapper = mount(ModelCard, { props: { entry: entry(name, platform, withOriginal) } })
      expect(wrapper.find('.model-prices').text()).toContain('输入$2.50')
      expect(wrapper.find('[data-price-field="input_price"] .model-original-price').text()).toBe('$5.00')
      expect(wrapper.find('.price-caption').text()).toBe('· 最低实付价')
      wrapper.unmount()
    }
  )
})


it('卡片实付人民币价格响应币种和充值比例切换', async () => {
  const value = entry('gpt-5.5', 'openai')
  const wrapper = mount(ModelCard, { props: { entry: value, currency: 'cny', creditsPerCny: 10 } })
  const row = () => wrapper.find('[data-price-field="cache_read_price"]')
  expect(row().find('strong').text()).toBe('¥0.02')
  expect(row().find('.model-original-price').text()).toBe('$0.40')
  await wrapper.setProps({ currency: 'usd' })
  expect(row().find('strong').text()).toBe('$0.20')
  wrapper.unmount()
})


it('video cards show a per-second price using the video-specific rate', async () => {
  const value = entry('muse-video', 'grok', false)
  value.offers[0].model.pricing!.billing_mode = 'video'
  value.offers[0].model.pricing!.per_request_price = 0.1
  value.offers[0].group.video_rate_independent = true
  value.offers[0].group.video_rate_multiplier = 0.5
  const wrapper = mount(ModelCard, { props: { entry: value } })
  try {
    expect(wrapper.find('[data-billing-mode="video"]').text()).toContain('$0.05')
    expect(wrapper.text()).toContain('modelPlaza.table.perUnitSecond')
    expect(wrapper.text()).not.toContain('/ 次')
    await wrapper.setProps({ currency: 'cny', creditsPerCny: 10 })
    expect(wrapper.find('[data-billing-mode="video"]').text()).toContain('¥0.005')
  } finally {
    wrapper.unmount()
  }
})

it('mixed per-request and per-second offers display separate prices', () => {
  const value = entry('muse-video', 'grok', false)
  value.offers[0].model.pricing!.billing_mode = 'video'
  value.offers[0].model.pricing!.per_request_price = 0.2
  const perRequest = structuredClone(value.offers[0])
  perRequest.group.id = 2
  perRequest.model.pricing!.billing_mode = 'per_request'
  perRequest.model.pricing!.per_request_price = 0.01
  value.offers.push(perRequest)
  const wrapper = mount(ModelCard, { props: { entry: value } })
  try {
    expect(wrapper.find('[data-billing-mode="video"]').text()).toContain('$0.10')
    expect(wrapper.find('[data-billing-mode="video"]').text()).toContain('modelPlaza.table.perUnitSecond')
    expect(wrapper.find('[data-billing-mode="per_request"]').text()).toContain('$0.005')
    expect(wrapper.find('[data-billing-mode="per_request"]').text()).toContain('modelPlaza.table.perUnitRequest')
    expect(wrapper.findAll('.model-media')).toHaveLength(2)
  } finally {
    wrapper.unmount()
  }
})
