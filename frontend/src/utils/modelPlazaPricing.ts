import type { PlazaModel } from '@/api/modelPlaza'
import { resolveIntervalPrices } from '@/utils/pricing'

export type ModelPlazaTokenPriceKey =
  | 'input_price'
  | 'output_price'
  | 'cache_read_price'
  | 'cache_write_price'
  | 'cache_write_1h_price'

/** Only known Claude/GPT originals belong in the official-price column. */
export function modelPlazaOriginalPricing(model: PlazaModel): PlazaModel['official_pricing'] {
  if ((model.pricing?.billing_mode || 'token') !== 'token') return null
  const name = model.name.trim().toLowerCase().split('/').at(-1) || ''
  const isClaude = /^(?:anthropic[.:])?claude(?:[-._]|\d|$)/.test(name)
  const isGPT = /^(?:openai[.:])?(?:gpt(?:[-._]|$)|chatgpt-|o[134](?:-|$)|codex(?:-|$))/.test(name)
  if (!isClaude && !isGPT) return null
  const pricing = model.official_pricing
  return pricing && [pricing.input_price, pricing.output_price].some(
    value => value != null && Number.isFinite(value) && value >= 0
  ) ? pricing : null
}

/** Other token models use configured channel/group prices, before any rate. */
export function modelPlazaReferencePricing(model: PlazaModel): PlazaModel['official_pricing'] {
  if ((model.pricing?.billing_mode || 'token') !== 'token') return null
  const original = modelPlazaOriginalPricing(model)
  if (original) return original
  const pricing = model.pricing
  if (!pricing) return null
  return {
    input_price: pricing.input_price,
    output_price: pricing.output_price,
    cache_read_price: pricing.cache_read_price,
    cache_write_price: pricing.cache_write_price,
    cache_write_1h_price: pricing.cache_write_1h_price,
    intervals: pricing.intervals?.map(interval => resolveIntervalPrices(interval, pricing))
  }
}

/** Change only the reference column; paid prices and their tiers stay intact. */
export function modelPlazaDisplayModel(model: PlazaModel): PlazaModel {
  if ((model.pricing?.billing_mode || 'token') !== 'token') return model
  return { ...model, official_pricing: modelPlazaReferencePricing(model) }
}
