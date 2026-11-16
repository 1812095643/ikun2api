import { normalizeVisibleMethod } from '@/components/payment/paymentFlow'

export function resolvePaymentFeeRate(defaultRate: number, rates: Record<string, number> | undefined, method: string): number {
  const value = rates?.[normalizeVisibleMethod(method) || method.trim()] ?? defaultRate
  return Number.isFinite(value) && value >= 0 && value <= 100 ? value : 0
}
