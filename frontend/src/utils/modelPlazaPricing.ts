import type { PlazaModel } from '@/api/modelPlaza'
import type { BillingMode } from '@/constants/channel'
import { formatScaled, resolveIntervalPrices } from '@/utils/pricing'

/** Billing units are shared by cards and detailed tables. Video prices are per second. */
const billingUnits: Record<BillingMode, { label: string; suffix: string }> = {
  token: { label: 'perUnitToken', suffix: 'perUnitToken' },
  per_request: { label: 'perRequest', suffix: 'perUnitRequest' },
  image: { label: 'perImage', suffix: 'perUnitImage' },
  video: { label: 'perSecond', suffix: 'perUnitSecond' }
}

export function modelPlazaBillingUnit(mode: BillingMode) {
  return billingUnits[mode] ?? billingUnits.token
}

export type ModelPlazaTokenPriceKey =
  | 'input_price'
  | 'output_price'
  | 'cache_read_price'
  | 'cache_write_price'
  | 'cache_write_1h_price'

/** Known Claude/GPT originals provide the model plaza display baseline. */
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

/** Plaza-only projection: known Claude/GPT prices never use channel overrides. */
export function modelPlazaDisplayModel(model: PlazaModel, longContext = true): PlazaModel {
  if ((model.pricing?.billing_mode || 'token') !== 'token') return model
  const original = modelPlazaOriginalPricing(model)
  if (!original) return { ...model, official_pricing: modelPlazaReferencePricing(model) }
  const valid = (value: number | null | undefined) =>
    value != null && Number.isFinite(value) && value >= 0 ? value : null
  const intervals = longContext && (original.intervals?.length ?? 0) > 1 ? (original.intervals ?? []) : []
  return {
    ...model,
    official_pricing: original,
    pricing: {
      ...model.pricing,
      billing_mode: 'token',
      input_price: valid(original.input_price),
      output_price: valid(original.output_price),
      cache_read_price: valid(original.cache_read_price),
      cache_write_price: valid(original.cache_write_price),
      cache_write_1h_price: valid(original.cache_write_1h_price),
      image_input_price: null,
      image_output_price: null,
      per_request_price: null,
      intervals
    },
    long_context_basis: intervals?.length ? 'whole_request' : undefined
  }
}

export type ModelPlazaCurrency = 'usd' | 'cny'

export function validModelPlazaRechargeRatio(value: number | undefined): boolean {
  return value != null && Number.isFinite(value) && value > 0
}

/** CNY is a balance-credit conversion, never an exchange-rate conversion. */
export function modelPlazaPaidPrice(
  value: number,
  scale: number,
  currency: ModelPlazaCurrency = 'usd',
  creditsPerCny = 0
): string {
  if (!Number.isFinite(value) || value < 0) return '—'
  if (currency === 'cny') {
    if (!validModelPlazaRechargeRatio(creditsPerCny)) return '—'
    return formatScaled(value / creditsPerCny, scale, 2).replace('$', '¥')
  }
  return formatScaled(value, scale, 2)
}
