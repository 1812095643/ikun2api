export type ForcedOpenAIServiceTier = '' | 'priority' | 'ultrafast'
export interface ServiceTierRateDraft { model: string; fast: string; ultrafast: string }
export interface OpenAIServiceTierDraft {
  fastEnabled: boolean
  ultrafastEnabled: boolean
  fast: string
  ultrafast: string
  models: ServiceTierRateDraft[]
}
interface TierRates { fast_multiplier?: number; ultrafast_multiplier?: number }
export interface OpenAIAccountServiceTiers extends TierRates {
  fast_enabled: boolean
  ultrafast_enabled: boolean
  model_multipliers?: Record<string, TierRates>
}

export function readOpenAIServiceTierDraft(extra?: Record<string, unknown> | null): OpenAIServiceTierDraft {
  const config = (extra?.openai_service_tiers ?? {}) as Partial<OpenAIAccountServiceTiers>
  const models = Object.entries(config.model_multipliers ?? {}).map(([model, rates]) => ({
    model, fast: rates.fast_multiplier == null ? '' : String(rates.fast_multiplier),
    ultrafast: rates.ultrafast_multiplier == null ? '' : String(rates.ultrafast_multiplier),
  }))
  for (const model of ['gpt-6-astra', 'gpt-6.1-sol']) {
    if (!models.some(row => row.model === model)) models.push({ model, fast: '', ultrafast: '' })
  }
  return {
    fastEnabled: config.fast_enabled !== false, ultrafastEnabled: config.ultrafast_enabled !== false,
    fast: config.fast_multiplier == null ? '' : String(config.fast_multiplier),
    ultrafast: config.ultrafast_multiplier == null ? '' : String(config.ultrafast_multiplier), models,
  }
}

export function serializeOpenAIServiceTierDraft(draft: OpenAIServiceTierDraft): OpenAIAccountServiceTiers {
  const parse = (raw: string): number | undefined => {
    if (!raw.trim()) return undefined
    const value = Number(raw)
    if (!Number.isFinite(value) || value <= 0 || value > 1000 || Math.abs(value * 1e4 - Math.round(value * 1e4)) > 1e-7) {
      throw new Error('Invalid service tier multiplier')
    }
    return value
  }
  const rates = (row: Pick<ServiceTierRateDraft, 'fast' | 'ultrafast'>): TierRates => ({
    ...(parse(row.fast) == null ? {} : { fast_multiplier: parse(row.fast) }),
    ...(parse(row.ultrafast) == null ? {} : { ultrafast_multiplier: parse(row.ultrafast) }),
  })
  const modelMultipliers: Record<string, TierRates> = Object.create(null)
  const seen = new Set<string>()
  for (const row of draft.models) {
    const model = row.model.trim().toLowerCase()
    const entry = rates(row)
    if (!model && !Object.keys(entry).length) continue
    if (!model || seen.has(model)) throw new Error('Invalid or duplicate service tier model')
    seen.add(model)
    if (Object.keys(entry).length) modelMultipliers[model] = entry
  }
  return {
    fast_enabled: draft.fastEnabled, ultrafast_enabled: draft.ultrafastEnabled, ...rates(draft),
    ...(Object.keys(modelMultipliers).length ? { model_multipliers: modelMultipliers } : {}),
  }
}

export function defaultUltrafastMultiplier(model: string): number | undefined {
  const value = model.trim().toLowerCase().replace(/_/g, '-').replace(/\./g, '-')
  if (/(?:^|\/)gpt-6-?1-sol(?:$|-)/.test(value)) return 8
  if (/(?:^|\/)gpt-6-astra(?:$|-)/.test(value)) return 6
  return undefined
}
