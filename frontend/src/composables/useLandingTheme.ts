import { onMounted, onUnmounted, shallowRef } from 'vue'

/** Uses the same persisted theme as the console; the landing artwork stays shared. */
export function useLandingTheme() {
  const isDark = shallowRef(document.documentElement.classList.contains('dark'))
  let preference: MediaQueryList | undefined
  let observer: MutationObserver | undefined

  function savedTheme() {
    try { return localStorage.getItem('theme') } catch { return null }
  }

  function apply(dark: boolean) {
    isDark.value = dark
    document.documentElement.classList.toggle('dark', dark)
  }

  function setTheme(dark: boolean) {
    apply(dark)
    try { localStorage.setItem('theme', dark ? 'dark' : 'light') } catch { /* Session-only preference. */ }
  }

  function sync() {
    const saved = savedTheme()
    apply(saved === 'dark' || (saved !== 'light' && !!preference?.matches))
  }

  function onStorage(event: StorageEvent) {
    if (event.key === 'theme' || event.key === null) sync()
  }

  onMounted(() => {
    preference = window.matchMedia('(prefers-color-scheme: dark)')
    sync()
    preference.addEventListener?.('change', sync)
    window.addEventListener('storage', onStorage)
    observer = new MutationObserver(() => {
      isDark.value = document.documentElement.classList.contains('dark')
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  })

  onUnmounted(() => {
    preference?.removeEventListener?.('change', sync)
    window.removeEventListener('storage', onStorage)
    observer?.disconnect()
  })

  return { isDark, setTheme, toggleTheme: () => setTheme(!isDark.value) }
}
