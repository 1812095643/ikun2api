import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PaymentFeeSettings from '../PaymentFeeSettings.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('支付方式独立手续费', () => {
  it('明确零费率与留空继承不同，并保留其他方式的费率', async () => {
    const wrapper = mount(PaymentFeeSettings, { props: { modelValue: { alipay: 0.6, stripe: 3 }, defaultRate: 1 } })
    await wrapper.get('#payment-fee-wxpay').setValue(0)
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([{ alipay: 0.6, wxpay: 0, stripe: 3 }])
    await wrapper.get('#payment-fee-alipay').setValue('')
    expect(wrapper.emitted('update:modelValue')?.[1]).toEqual([{ stripe: 3 }])
    await wrapper.get('#payment-fee-default').setValue('0.29')
    expect(wrapper.emitted('update:defaultRate')?.[0]).toEqual([0.29])
    expect(wrapper.findAll('img')).toHaveLength(2)
    wrapper.unmount()
  })
})
