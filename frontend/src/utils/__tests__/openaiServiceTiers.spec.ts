import { describe, expect, it } from 'vitest'
import { defaultUltrafastMultiplier, readOpenAIServiceTierDraft, serializeOpenAIServiceTierDraft } from '../openaiServiceTiers'

describe('OpenAI account service tiers', () => {
  it('preserves existing enabled capabilities without imposing price overrides', () => {
    const draft = readOpenAIServiceTierDraft()
    expect(draft.fastEnabled).toBe(true)
    expect(draft.ultrafastEnabled).toBe(true)
    expect(serializeOpenAIServiceTierDraft(draft)).toEqual({ fast_enabled: true, ultrafast_enabled: true })
    expect(defaultUltrafastMultiplier('gpt-6-astra')).toBe(6)
    expect(defaultUltrafastMultiplier('gpt-6.1-sol')).toBe(8)
  })
  it('roundtrips per-account and per-model prices independently', () => {
    const extra = { openai_service_tiers: { fast_enabled: false, ultrafast_enabled: true, fast_multiplier: 2, model_multipliers: { 'gpt-6-astra': { ultrafast_multiplier: 6 }, 'gpt-6.1-sol': { ultrafast_multiplier: 8 } } } }
    expect(serializeOpenAIServiceTierDraft(readOpenAIServiceTierDraft(extra))).toEqual(extra.openai_service_tiers)
  })
  it('rejects invalid or duplicate prices before saving', () => {
    for (const value of ['0', '-1', 'Infinity', 'NaN', '1001', '1.12345']) {
      const draft = readOpenAIServiceTierDraft(); draft.fast = value
      expect(() => serializeOpenAIServiceTierDraft(draft)).toThrow()
    }
    const draft = readOpenAIServiceTierDraft(); draft.fast = '2.1234'
    expect(serializeOpenAIServiceTierDraft(draft).fast_multiplier).toBe(2.1234)
    draft.models = [{ model: 'gpt-6-astra', fast: '', ultrafast: '6' }, { model: 'gpt-6-astra', fast: '2', ultrafast: '' }]
    expect(() => serializeOpenAIServiceTierDraft(draft)).toThrow()
  })
  it('does not mutate live account extras while editing a draft', () => {
    const extra = { openai_service_tiers: { fast_enabled: true, ultrafast_enabled: false, model_multipliers: { 'gpt-*': { fast_multiplier: 2 } } } }
    const original = JSON.stringify(extra)
    const draft = readOpenAIServiceTierDraft(extra); draft.models[0].fast = '3'; draft.ultrafastEnabled = true
    expect(JSON.stringify(extra)).toBe(original)
  })
})
