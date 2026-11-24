import { onUnmounted, ref } from 'vue'
import { isCancel } from 'axios'
import { listConnectionTraces, getConnectionTrace, type ConnectionPage, type ConnectionQuery, type ConnectionTrace } from '@/api/admin/connectionDiagnostics'

export function useConnectionDiagnostics() {
  const result = ref<ConnectionPage | null>(null), selected = ref<ConnectionTrace | null>(null)
  const loading = ref(false), detailLoading = ref(false), error = ref(false), detailError = ref(false)
  const cursors = ref<string[]>([''])
  let listController: AbortController | undefined, detailController: AbortController | undefined
  async function load(query: ConnectionQuery, direction: 'reset' | 'next' | 'previous' = 'reset') {
    listController?.abort(); const controller = new AbortController(); listController = controller
    const nextCursors = direction === 'reset' ? [''] : [...cursors.value]
    if (direction === 'next' && result.value?.next_cursor) nextCursors.push(result.value.next_cursor)
    if (direction === 'previous' && nextCursors.length > 1) nextCursors.pop()
    loading.value = true; error.value = false
    try { const data = await listConnectionTraces({ ...query, cursor: nextCursors[nextCursors.length - 1] || undefined, limit: 50 }, controller.signal); if (!controller.signal.aborted) { result.value = data; cursors.value = nextCursors } }
    catch (err) { if (!controller.signal.aborted && !isCancel(err)) { error.value = true; result.value = null } }
    finally { if (listController === controller) loading.value = false }
  }
  async function open(row: ConnectionTrace) {
    detailController?.abort(); const controller = new AbortController(); detailController = controller
    selected.value = row; detailError.value = false; detailLoading.value = false
    if (!row.detail_available) return
    detailLoading.value = true
    try { const detail = await getConnectionTrace(row, controller.signal); if (!controller.signal.aborted) selected.value = detail }
    catch (err) { if (!controller.signal.aborted && !isCancel(err)) detailError.value = true }
    finally { if (detailController === controller) detailLoading.value = false }
  }
  function close() { detailController?.abort(); detailLoading.value = false; selected.value = null }
  onUnmounted(() => { listController?.abort(); detailController?.abort() })
  return { result, selected, loading, detailLoading, error, detailError, cursors, load, open, close }
}
