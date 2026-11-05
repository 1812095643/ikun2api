import { computed, onMounted, onScopeDispose, shallowRef, watch } from 'vue'

/** Recursive timeout avoids overlapping polling; hidden/offline pages pause. */
export function usePoolRefresh(refresh: () => Promise<boolean>, busy: () => boolean) {
  const interval = shallowRef('30')
  const online = shallowRef(typeof navigator === 'undefined' || navigator.onLine)
  const visible = shallowRef(typeof document === 'undefined' || !document.hidden)
  const paused = computed(() => !online.value || !visible.value || interval.value === '0')
  let timer: ReturnType<typeof setTimeout> | undefined
  let failures = 0
  let running = false
  let disposed = false

  function clear() { if (timer) clearTimeout(timer); timer = undefined }
  function schedule() {
    clear()
    if (disposed || paused.value) return
    timer = setTimeout(tick, Math.min(120, Number(interval.value) * 2 ** failures) * 1000)
  }
  async function tick() {
    clear()
    if (disposed || paused.value) return
    if (running || busy()) { schedule(); return }
    running = true
    try { failures = await refresh() ? 0 : Math.min(failures + 1, 3) }
    catch { failures = Math.min(failures + 1, 3) }
    finally { running = false; schedule() }
  }
  function environmentChanged() {
    const wasUnavailable = !visible.value || !online.value
    visible.value = !document.hidden
    online.value = navigator.onLine
    if (paused.value) clear()
    else if (wasUnavailable) void tick()
    else schedule()
  }
  watch(interval, () => { failures = 0; schedule() })
  onMounted(() => {
    document.addEventListener('visibilitychange', environmentChanged)
    window.addEventListener('online', environmentChanged)
    window.addEventListener('offline', environmentChanged)
    schedule()
  })
  onScopeDispose(() => {
    disposed = true
    clear()
    document.removeEventListener('visibilitychange', environmentChanged)
    window.removeEventListener('online', environmentChanged)
    window.removeEventListener('offline', environmentChanged)
  })
  return { interval, paused, online }
}
