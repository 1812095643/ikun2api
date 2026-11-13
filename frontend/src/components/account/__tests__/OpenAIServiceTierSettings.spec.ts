import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import OpenAIServiceTierSettings from '../OpenAIServiceTierSettings.vue'
import { readOpenAIServiceTierDraft } from '@/utils/openaiServiceTiers'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('OpenAIServiceTierSettings', () => {
  it('edits capabilities and prices through independent updates', async () => {
    const modelValue = readOpenAIServiceTierDraft()
    const wrapper = mount(OpenAIServiceTierSettings, { props: { modelValue } })
    const switches = wrapper.findAll('input[type="checkbox"]')
    expect(switches).toHaveLength(2)
    expect((switches[0].element as HTMLInputElement).checked).toBe(true)
    await switches[0].setValue(false)
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toMatchObject({ fastEnabled: false, ultrafastEnabled: true })
    const prices = wrapper.findAll('input[type="number"]')
    await prices[0].setValue('2')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toMatchObject({ fast: '2' })
    expect(modelValue.fast).toBe('')
  })
})
