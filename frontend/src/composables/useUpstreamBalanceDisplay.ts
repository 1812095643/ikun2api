import { shallowRef } from 'vue'
import { defaultBalanceDisplay, upstreamBalancesAPI, type UpstreamBalanceDisplay } from '@/api/upstreamBalances'
import { useAppStore } from '@/stores/app'

export function useUpstreamBalanceDisplay() {
  const app = useAppStore()
  const display = shallowRef(defaultBalanceDisplay())
  const displaySaving = shallowRef(false)
  const displayReady = shallowRef(false)
  const displayError = shallowRef('')
  let confirmed = display.value
  let pending: UpstreamBalanceDisplay | null = null
  let active = false

  function initializeDisplay(value?: UpstreamBalanceDisplay) {
    if (displayReady.value) return
    confirmed = { ...defaultBalanceDisplay(), ...value }
    display.value = confirmed
    displayReady.value = true
  }
  async function persist() {
    if (active) return
    active = true
    displaySaving.value = true
    try {
      while (pending) {
        const next = pending
        pending = null
        try {
          confirmed = await upstreamBalancesAPI.saveDisplay(next)
          displayError.value = ''
        } catch {
          pending = null
          display.value = confirmed
          displayError.value = '列表设置保存失败，已恢复上次设置。'
          app.showError(displayError.value)
        }
      }
    } finally { active = false; displaySaving.value = false }
  }
  function updateDisplay(patch: Partial<UpstreamBalanceDisplay>) {
    if (!displayReady.value) return
    display.value = { ...display.value, ...patch }
    pending = display.value
    void persist()
  }
  function setHidden(ids: string[], hidden: boolean) {
    const set = new Set(display.value.hidden)
    for (const id of ids) { if (hidden) set.add(id); else set.delete(id) }
    updateDisplay({ hidden: [...set] })
  }
  return { display, displaySaving, displayReady, displayError, initializeDisplay, updateDisplay, setHidden }
}
