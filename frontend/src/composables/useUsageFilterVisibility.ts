import { computed, ref, toValue, watch, type MaybeRefOrGetter } from 'vue'

function preferenceOwner(): string {
  try {
    const user = JSON.parse(localStorage.getItem('auth_user') || 'null')
    return Number.isSafeInteger(user?.id) && user.id > 0 ? String(user.id) : 'browser'
  } catch {
    return 'browser'
  }
}

// Store display preferences only. Filters, credentials and request data are
// never written here. New filters remain visible when first introduced.
export function useUsageFilterVisibility(scope: 'admin' | 'user', mode: MaybeRefOrGetter<string>) {
  const owner = preferenceOwner()
  const key = computed(() => `usage-filter-visibility:${scope}:${owner}:${toValue(mode)}`)
  const hidden = ref<string[]>([])
  watch(key, value => {
    try {
      const saved: unknown = JSON.parse(localStorage.getItem(value) || '[]')
      hidden.value = Array.isArray(saved) ? [...new Set(saved.filter((item): item is string => typeof item === 'string'))] : []
    } catch {
      hidden.value = []
    }
  }, { immediate: true })
  function persist() {
    try { localStorage.setItem(key.value, JSON.stringify(hidden.value)) } catch { /* Storage may be disabled; the current view still works. */ }
  }
  function isVisible(name: string) { return !hidden.value.includes(name) }
  function toggle(name: string): boolean {
    hidden.value = isVisible(name) ? [...hidden.value, name] : hidden.value.filter(item => item !== name)
    persist()
    return isVisible(name)
  }
  function reveal(name: string) {
    if (isVisible(name)) return
    hidden.value = hidden.value.filter(item => item !== name)
    persist()
  }
  function reset() { hidden.value = []; persist() }
  return { hidden, isVisible, toggle, reveal, reset }
}
