import { describe, expect, it } from 'vitest'
import { mediaPrice, modelPrice, modelOfficialPrice, type ModelEntry } from './useModelSquare'
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'
import type { ModelPlazaTokenPriceKey } from '@/utils/modelPlazaPricing'

function entry(
  group: Partial<ModelPlazaGroup>,
  pricing: NonNullable<PlazaModel['pricing']>
): ModelEntry {
  return {
    name: 'test',
    platform: 'openai',
    offers: [{
      group: { rate_multiplier: 2, ...group } as ModelPlazaGroup,
      model: { name: 'test', platform: 'openai', pricing, official_pricing: null }
    }]
  }
}

const priceCases: [ModelPlazaTokenPriceKey, string, string, string][] = [
  ['input_price', '$2.50', '$4.00', '$5.00'],
  ['output_price', '$10.00', '$20.00', '$20.00'],
  ['cache_read_price', '$0.50', '$0.40', '$1.00'],
  ['cache_write_price', '$3.00', '$5.00', '$6.00'],
  ['cache_write_1h_price', '$6.00', '$8.00', '$12.00']
]

function pricedModel(name: string, platform: string): ModelEntry {
  const value = entry({ rate_multiplier: 2, user_rate_multiplier: 0.5 }, {
    billing_mode: 'token', input_price: 5e-6, output_price: 20e-6,
    cache_read_price: 1e-6, cache_write_price: 6e-6, cache_write_1h_price: 12e-6
  } as NonNullable<PlazaModel['pricing']>)
  value.name = name
  value.platform = platform
  value.offers[0].model = {
    ...value.offers[0].model, name, platform,
    official_pricing: {
      input_price: 4e-6, output_price: 20e-6,
      cache_read_price: 0.4e-6, cache_write_price: 5e-6, cache_write_1h_price: 8e-6
    }
  }
  return value
}

describe('原生模型广场计价', () => {
  it.each([
    ['claude-sonnet-5-5', 'anthropic'], ['gpt-5.5', 'openai'],
    ['gpt-5.5', 'openai_bps'], ['openai/gpt-5.5', 'openai'],
    ['anthropic.claude-sonnet-4-5', 'anthropic'], ['o3', 'openai']
  ])('%s 只在官方列使用原价，实付仍按渠道价乘个人倍率', (name, platform) => {
    const value = pricedModel(name, platform)
    for (const [key, paid, original] of priceCases) {
      expect(modelPrice(value, key)).toBe(paid)
      expect(modelOfficialPrice(value, key)).toBe(original)
    }
    const paidBefore = priceCases.map(([key]) => modelPrice(value, key))
    value.offers[0].model.official_pricing = null
    expect(priceCases.map(([key]) => modelPrice(value, key))).toEqual(paidBefore)
    for (const [key, , , channel] of priceCases) expect(modelOfficialPrice(value, key)).toBe(channel)
    expect(value.offers[0].model.pricing?.input_price).toBe(5e-6)
  })

  it.each([
    ['gemini-2.5-pro', 'gemini'], ['deepseek-chat', 'deepseek'],
    ['glm-5.2', 'openai'], ['minimax-m3', 'anthropic']
  ])('%s 官方列显示渠道基价，忽略目录价且不乘倍率', (name, platform) => {
    const value = pricedModel(name, platform)
    for (const [key, paid, , channel] of priceCases) {
      expect(modelPrice(value, key)).toBe(paid)
      expect(modelOfficialPrice(value, key)).toBe(channel)
    }
  })

  it.each([['claude-unlisted-private-model', 'anthropic'], ['gpt-unlisted', 'openai']])(
    '%s 没有原价时官方列回退渠道价，保留零价与缺失值', (name, platform) => {
      const value = pricedModel(name, platform)
      value.offers[0].model.official_pricing = null
      for (const [key, paid, , channel] of priceCases) {
        expect(modelPrice(value, key)).toBe(paid)
        expect(modelOfficialPrice(value, key)).toBe(channel)
      }
      value.offers[0].model.pricing!.input_price = 0
      expect(modelOfficialPrice(value, 'input_price')).toBe('$0.00')
      value.offers[0].model.pricing!.output_price = null
      expect(modelPrice(value, 'output_price')).toBe('—')
      expect(modelOfficialPrice(value, 'output_price')).toBe('—')
    }
  )

  it('目录价无效时回退渠道价，零原价有效且不改变实付', () => {
    const value = pricedModel('gpt-5.5', 'openai')
    value.offers[0].model.official_pricing!.input_price = NaN
    value.offers[0].model.official_pricing!.output_price = -1
    expect(modelOfficialPrice(value, 'input_price')).toBe('$5.00')
    value.offers[0].model.official_pricing!.input_price = 0
    expect(modelOfficialPrice(value, 'input_price')).toBe('$0.00')
    expect(modelPrice(value, 'input_price')).toBe('$2.50')
  })

  it('已知原价缺缓存项时官方列留空，实付仍显示渠道对应价', () => {
    const value = pricedModel('claude-sonnet-5-5', 'anthropic')
    value.offers[0].model.official_pricing!.cache_write_price = null
    value.offers[0].model.official_pricing!.cache_write_1h_price = null
    expect(modelPrice(value, 'cache_write_price')).toBe('$3.00')
    expect(modelPrice(value, 'cache_write_1h_price')).toBe('$6.00')
    expect(modelOfficialPrice(value, 'cache_write_price')).toBe('—')
    expect(modelOfficialPrice(value, 'cache_write_1h_price')).toBe('—')
  })

  it('多分组实付取渠道价乘各自倍率的最低值，官方价不受排序影响', () => {
    const value = pricedModel('gpt-5.5', 'openai')
    const cheaper = structuredClone(value.offers[0])
    cheaper.group.user_rate_multiplier = 0.01
    cheaper.model.pricing!.input_price = 1e-6
    cheaper.model.official_pricing = null
    value.offers.unshift(cheaper)
    for (let order = 0; order < 2; order++) {
      expect(modelPrice(value, 'input_price')).toBe('$0.01')
      expect(modelOfficialPrice(value, 'input_price')).toBe('$4.00')
      value.offers.reverse()
    }
  })

  it('无原价的多分组参考价取渠道基价，不使用最低实付价', () => {
    const value = pricedModel('deepseek-chat', 'deepseek')
    const other = structuredClone(value.offers[0])
    other.model.pricing!.input_price = 2e-6
    other.group.user_rate_multiplier = 10
    value.offers.push(other)
    expect(modelPrice(value, 'input_price')).toBe('$2.50')
    expect(modelOfficialPrice(value, 'input_price')).toBe('$2.00')
  })

  it('按张计费不使用官方 token 价', () => {
    const value = pricedModel('gpt-image-2', 'openai')
    value.offers[0].model.pricing!.billing_mode = 'image'
    value.offers[0].model.pricing!.per_request_price = 0.4
    expect(modelPrice(value, 'input_price')).toBe('—')
    expect(modelOfficialPrice(value, 'input_price')).toBe('—')
    expect(mediaPrice(value)).toBe('$0.20')
  })

  it('个人倍率覆盖分组默认倍率，无个人倍率时沿用分组倍率', () => {
    const value = pricedModel('claude-sonnet-5-5', 'anthropic')
    expect(modelPrice(value, 'input_price')).toBe('$2.50')
    delete value.offers[0].group.user_rate_multiplier
    expect(modelPrice(value, 'input_price')).toBe('$10.00')
    expect(modelOfficialPrice(value, 'input_price')).toBe('$4.00')
  })

  it('零倍率只让实付免费，不改变参考价或把缺失项显示为免费', () => {
    const value = pricedModel('claude-sonnet-5-5', 'anthropic')
    value.offers[0].group.user_rate_multiplier = 0
    expect(modelPrice(value, 'input_price')).toBe('$0.00')
    expect(modelOfficialPrice(value, 'input_price')).toBe('$4.00')
    value.offers[0].model.pricing!.output_price = null
    expect(modelPrice(value, 'output_price')).toBe('—')
  })

  it('图片独立倍率覆盖通用倍率并保留按张单位', () => {
    expect(mediaPrice(entry({ image_rate_independent: true, image_rate_multiplier: 0.5 }, {
      billing_mode: 'image', per_request_price: 0.4
    } as NonNullable<PlazaModel['pricing']>))).toBe('$0.20')
  })
})
