import { describe, expect, it } from 'vitest'
import { mediaPrice, modelPrice, modelOfficialPrice, type ModelEntry } from './useModelSquare'
import { modelPlazaDisplayModel, modelPlazaPaidPrice, type ModelPlazaTokenPriceKey } from '@/utils/modelPlazaPricing'
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'

const cases: [ModelPlazaTokenPriceKey, string, string, string][] = [
  ['input_price', '$2.00', '$4.00', '$2.50'],
  ['output_price', '$10.00', '$20.00', '$10.00'],
  ['cache_read_price', '$0.20', '$0.40', '$0.50'],
  ['cache_write_price', '$2.50', '$5.00', '$3.00'],
  ['cache_write_1h_price', '$4.00', '$8.00', '$6.00']
]

function pricedModel(name = 'claude-sonnet-5-5', platform = 'anthropic'): ModelEntry {
  return {
    name, platform,
    offers: [{
      group: { rate_multiplier: 2, user_rate_multiplier: 0.5 } as ModelPlazaGroup,
      model: {
        name, platform,
        pricing: {
          billing_mode: 'token', input_price: 5e-6, output_price: 20e-6,
          cache_read_price: 1e-6, cache_write_price: 6e-6, cache_write_1h_price: 12e-6,
          image_input_price: null, image_output_price: null, per_request_price: null
        },
        official_pricing: {
          input_price: 4e-6, output_price: 20e-6,
          cache_read_price: 0.4e-6, cache_write_price: 5e-6, cache_write_1h_price: 8e-6
        }
      }
    }]
  }
}

describe('模型广场展示价格', () => {
  it.each([
    ['claude-sonnet-5-5', 'anthropic'], ['gpt-5.5', 'openai'],
    ['gpt-5.5', 'openai_bps'], ['openai/gpt-5.5', 'openai'],
    ['anthropic.claude-sonnet-4-5', 'anthropic'], ['o3', 'openai']
  ])('%s 五项实付均按官方原价乘个人倍率，忽略渠道改价', (name, platform) => {
    const value = pricedModel(name, platform)
    for (const [key, paid, original] of cases) {
      expect(modelPrice(value, key)).toBe(paid)
      expect(modelOfficialPrice(value, key)).toBe(original)
    }
    // Projected display never mutates the billing/channel object.
    const model = value.offers[0].model
    expect(modelPlazaDisplayModel(model).pricing?.input_price).toBe(4e-6)
    expect(model.pricing?.input_price).toBe(5e-6)
    for (const [key, paid] of cases) expect(modelPrice(value, key, 'cny', 10)).toBe(paid.replace('$', '¥').replace(/([0-9.]+)/, amount => (Number(amount) / 10).toFixed(2)))
  })

  it.each([['gemini-2.5-pro', 'gemini'], ['deepseek-chat', 'deepseek'], ['glm-5.2', 'openai'], ['minimax-m3', 'anthropic']])(
    '%s 保留渠道基价与倍率', (name, platform) => {
      const value = pricedModel(name, platform)
      for (const [key, , , paid] of cases) expect(modelPrice(value, key)).toBe(paid)
      expect(modelOfficialPrice(value, 'input_price')).toBe('$5.00')
    }
  )

  it('整个模型没有原价时，仍允许沿用渠道基价', () => {
    const value = pricedModel('gpt-unlisted', 'openai')
    value.offers[0].model.official_pricing = null
    for (const [key, , , paid] of cases) expect(modelPrice(value, key)).toBe(paid)
    expect(modelOfficialPrice(value, 'cache_write_1h_price')).toBe('$12.00')
  })

  it('已知原价缺少缓存项时两列均留空，不能补入渠道价或5m价', () => {
    const value = pricedModel()
    value.offers[0].model.official_pricing!.cache_write_price = null
    value.offers[0].model.official_pricing!.cache_write_1h_price = null
    for (const key of ['cache_write_price', 'cache_write_1h_price'] as const) {
      expect(modelPrice(value, key)).toBe('—')
      expect(modelOfficialPrice(value, key)).toBe('—')
    }
    expect(modelPlazaDisplayModel(value.offers[0].model).pricing?.cache_write_1h_price).toBeNull()
  })

  it('跨分组取原价乘各自倍率的最低值，不被渠道价或排序影响', () => {
    const value = pricedModel('gpt-5.5', 'openai')
    const cheaper = structuredClone(value.offers[0])
    cheaper.group.user_rate_multiplier = 0.01
    cheaper.model.pricing!.input_price = 1e-6
    cheaper.model.official_pricing = null
    value.offers.unshift(cheaper)
    for (let i = 0; i < 2; i++) {
      expect(modelPrice(value, 'input_price')).toBe('$0.04')
      expect(modelOfficialPrice(value, 'input_price')).toBe('$4.00')
      value.offers.reverse()
    }
  })

  it('个人倍率优先，零倍率与零原价有效，缺失值不会被当成免费', () => {
    const value = pricedModel()
    delete value.offers[0].group.user_rate_multiplier
    expect(modelPrice(value, 'input_price')).toBe('$8.00')
    value.offers[0].group.user_rate_multiplier = 0
    expect(modelPrice(value, 'input_price')).toBe('$0.00')
    value.offers[0].model.official_pricing!.input_price = 0
    expect(modelOfficialPrice(value, 'input_price')).toBe('$0.00')
    value.offers[0].model.official_pricing!.output_price = null
    expect(modelPrice(value, 'output_price')).toBe('—')
  })

  it('token 参考原价不用于图片计费，图片使用独立倍率及人民币换算', () => {
    const value = pricedModel('gpt-image-2', 'openai')
    value.offers[0].model.pricing!.billing_mode = 'image'
    value.offers[0].model.pricing!.per_request_price = 0.4
    value.offers[0].group.image_rate_independent = true
    value.offers[0].group.image_rate_multiplier = 0.2
    expect(modelPrice(value, 'input_price')).toBe('—')
    expect(modelOfficialPrice(value, 'input_price')).toBe('—')
    expect(mediaPrice(value)).toBe('$0.08')
    expect(mediaPrice(value, 'cny', 10)).toBe('¥0.008')
  })

  it.each([0, -1, NaN, Infinity])('充值比例 %s 不产生虚假的人民币价格', ratio => {
    expect(modelPlazaPaidPrice(3, 1, 'cny', ratio)).toBe('—')
  })

  it('人民币按照充值比例相除；切回美元及官方美元价不受影响', () => {
    const value = pricedModel()
    expect(modelPrice(value, 'input_price', 'cny', 10)).toBe('¥0.20')
    expect(modelPrice(value, 'input_price', 'cny', 20)).toBe('¥0.10')
    expect(modelPrice(value, 'input_price', 'usd', 20)).toBe('$2.00')
    expect(modelOfficialPrice(value, 'input_price')).toBe('$4.00')
  })
})
