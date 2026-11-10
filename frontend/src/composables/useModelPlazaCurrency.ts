import { computed, shallowRef } from 'vue'
import { validModelPlazaRechargeRatio, type ModelPlazaCurrency } from '@/utils/modelPlazaPricing'

const storageKey = 'model-plaza-currency'

export function useModelPlazaCurrency(creditsPerCny: () => number | undefined) {
  let saved: ModelPlazaCurrency = 'usd'
  try { if (localStorage.getItem(storageKey) === 'cny') saved = 'cny' } catch { /* Storage may be unavailable. */ }
  const preference = shallowRef<ModelPlazaCurrency>(saved)
  const currency = computed<ModelPlazaCurrency>({
    get: () => preference.value === 'cny' && validModelPlazaRechargeRatio(creditsPerCny()) ? 'cny' : 'usd',
    set: value => {
      preference.value = value
      try { localStorage.setItem(storageKey, value) } catch { /* Keep the current selection in memory. */ }
    }
  })
  return { currency }
}
