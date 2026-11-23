import { computed, shallowRef } from 'vue'
import { useMutationObserver } from '@vueuse/core'

export function usePaymentChartTheme() {
  const dark = shallowRef(document.documentElement.classList.contains('dark'))
  useMutationObserver(document.documentElement, () => { dark.value = document.documentElement.classList.contains('dark') }, { attributes: true, attributeFilter: ['class'] })
  return computed(() => ({ text: dark.value ? '#cbd5e1' : '#64748b', grid: dark.value ? 'rgba(148,163,184,0.12)' : 'rgba(148,163,184,0.18)' }))
}
