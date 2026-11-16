import { describe, expect, it } from 'vitest'
import { resolvePaymentFeeRate } from '../paymentFees'

describe('支付手续费覆盖', () => {
  it('单独设置覆盖默认费率，保留零费率并兼容直连别名', () => {
    const rates = { alipay: 0.29, wxpay: 0 }
    expect(resolvePaymentFeeRate(0.6, rates, 'alipay')).toBe(0.29)
    expect(resolvePaymentFeeRate(0.6, rates, 'alipay_direct')).toBe(0.29)
    expect(resolvePaymentFeeRate(0.6, rates, 'wxpay')).toBe(0)
    expect(resolvePaymentFeeRate(0.6, rates, 'stripe')).toBe(0.6)
    expect(resolvePaymentFeeRate(0.6, undefined, 'wxpay')).toBe(0.6)
  })
})
