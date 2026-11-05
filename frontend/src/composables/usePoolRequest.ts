import { onScopeDispose, shallowRef } from 'vue'

/** Only the latest request may update data, errors or the loading state. */
export function usePoolRequest<T>(initial: T) {
  const data = shallowRef<T>(initial)
  const loading = shallowRef(false)
  const failed = shallowRef(false)
  const updatedAt = shallowRef<number | null>(null)
  let controller: AbortController | null = null
  let sequence = 0

  function cancel() {
    sequence++
    controller?.abort()
    controller = null
    loading.value = false
  }

  function reset() {
    cancel()
    data.value = initial
    failed.value = false
    updatedAt.value = null
  }

  async function run(request: (signal: AbortSignal) => Promise<T>): Promise<boolean> {
    cancel()
    const current = sequence
    const active = new AbortController()
    controller = active
    loading.value = true
    try {
      const result = await request(active.signal)
      if (current !== sequence || active.signal.aborted) return false
      data.value = result
      updatedAt.value = Date.now()
      failed.value = false
      return true
    } catch (error) {
      if (current !== sequence || active.signal.aborted) return false
      const name = (error as { name?: string })?.name
      if (name === 'CanceledError' || name === 'AbortError') return false
      failed.value = true
      return false
    } finally {
      if (current === sequence) {
        loading.value = false
        controller = null
      }
    }
  }

  onScopeDispose(cancel)
  return { data, loading, failed, updatedAt, run, cancel, reset }
}
