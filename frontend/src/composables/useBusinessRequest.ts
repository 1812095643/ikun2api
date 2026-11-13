import { onBeforeUnmount, ref, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { extractApiErrorMessage } from '@/utils/apiError'
export function useBusinessRequest<T>() {
  const data = shallowRef<T | null>(null)
  const loading = ref(false)
  const error = ref('')
  const { t } = useI18n()
  let controller: AbortController | null = null
  async function load(loader: (signal: AbortSignal) => Promise<T>) {
    controller?.abort()
    const current = new AbortController()
    controller = current
    loading.value = true
    error.value = ''
    try {
      const result = await loader(current.signal)
      if (!current.signal.aborted) {
        data.value = result
        return result
      }
    } catch (e) {
      if (!current.signal.aborted) {
        data.value = null
        error.value = extractApiErrorMessage(e, t('business.loadingFailed'))
      }
    } finally {
      if (!current.signal.aborted) loading.value = false
    }
    return null
  }
  function clear() {
    controller?.abort()
    data.value = null
    error.value = ''
    loading.value = false
  }
  onBeforeUnmount(() => controller?.abort())
  return { data, loading, error, load, clear }
}
