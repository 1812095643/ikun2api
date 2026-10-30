import { describe, expect, it } from 'vitest'
import { mediaPrice, modelPrice, type ModelEntry } from './useModelSquare'
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'

function entry(
  group: Partial<ModelPlazaGroup>,
  pricing: NonNullable<PlazaModel['pricing']>
): ModelEntry {
  return {
    name: 'test',
    platform: 'openai',
    offers: [
      {
        group: { rate_multiplier: 2, ...group } as ModelPlazaGroup,
        model: { name: 'test', platform: 'openai', pricing, official_pricing: null }
      }
    ]
  }
}
describe('原生模型广场计价', () => {
  it('个人倍率覆盖分组默认倍率', () => {
    expect(
      modelPrice(
        entry({ user_rate_multiplier: 1.5 }, {
          billing_mode: 'token',
          input_price: 0.000002
        } as NonNullable<PlazaModel['pricing']>),
        'input_price'
      )
    ).toBe('$3.00')
  })
  it('零倍率保留免费价格，缺失价格不展示为免费', () => {
    const value = entry({ user_rate_multiplier: 0 }, {
      billing_mode: 'token',
      input_price: 0.000002
    } as NonNullable<PlazaModel['pricing']>)
    expect(modelPrice(value, 'input_price')).toBe('$0.00')
    expect(modelPrice(value, 'output_price')).toBe('—')
  })
  it('图片独立倍率覆盖通用倍率并保留按张单位', () => {
    expect(
      mediaPrice(
        entry({ image_rate_independent: true, image_rate_multiplier: 0.5 }, {
          billing_mode: 'image',
          per_request_price: 0.4
        } as NonNullable<PlazaModel['pricing']>)
      )
    ).toBe('$0.20')
  })
})
