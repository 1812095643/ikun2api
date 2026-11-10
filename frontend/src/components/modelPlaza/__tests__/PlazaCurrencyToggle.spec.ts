import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import PlazaCurrencyToggle from '../PlazaCurrencyToggle.vue'
import { useModelPlazaCurrency } from '@/composables/useModelPlazaCurrency'

beforeEach(() => localStorage.clear())

describe('模型广场币种选择', () => {
  it('有充值比例时可切换，事件只更新所选币种', async () => {
    const wrapper = mount(PlazaCurrencyToggle, { props: { modelValue: 'usd', creditsPerCny: 10 } })
    const buttons = wrapper.findAll('button')
    expect(buttons[1].attributes('title')).toBe('1 元 = 10 额度')
    await buttons[1].trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([['cny']])
    await wrapper.setProps({ modelValue: 'cny' })
    expect(buttons[1].attributes('aria-pressed')).toBe('true')
    wrapper.unmount()
  })

  it('未设置比例时禁用人民币，延迟加载比例后恢复已保存的选择', () => {
    const wrapper = mount(PlazaCurrencyToggle, { props: { modelValue: 'usd', creditsPerCny: 0 } })
    expect(wrapper.findAll('button')[1].element.disabled).toBe(true)
    localStorage.setItem('model-plaza-currency', 'cny')
    const ratio = ref(0)
    const { currency } = useModelPlazaCurrency(() => ratio.value)
    expect(currency.value).toBe('usd')
    ratio.value = 10
    expect(currency.value).toBe('cny')
    currency.value = 'usd'
    expect(localStorage.getItem('model-plaza-currency')).toBe('usd')
    wrapper.unmount()
  })
})
