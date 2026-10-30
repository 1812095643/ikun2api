import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { refreshNavigation } from '@/utils/navigationEvents'
import {
  ticketAPI,
  workspaceError,
  type Ticket,
  type TicketDraft,
  type TicketPage,
  type TicketStatus
} from '@/api/workspace'

export function useTickets(admin: boolean) {
  const route = useRoute(),
    router = useRouter(),
    api = ticketAPI(admin)
  const filters = reactive({ page: 1, search: '', status: '' })
  const result = ref<TicketPage | null>(null),
    detail = ref<Ticket | null>(null)
  const loading = ref(false),
    detailLoading = ref(false),
    busy = ref(false),
    error = ref(''),
    creating = ref(false)
  const selected = computed(() => Number(route.params.id) || 0)
  let listRequest: AbortController | null = null,
    detailRequest: AbortController | null = null,
    timer: ReturnType<typeof setInterval> | undefined,
    debounce: ReturnType<typeof setTimeout> | undefined
  async function loadList(quiet = false) {
    listRequest?.abort()
    const request = new AbortController()
    listRequest = request
    if (!quiet) loading.value = true
    try {
      result.value = await api.list(filters, request.signal)
      error.value = ''
    } catch (e) {
      if (!request.signal.aborted) error.value = workspaceError(e)
    } finally {
      if (!request.signal.aborted) loading.value = false
    }
  }
  async function loadDetail(quiet = false) {
    detailRequest?.abort()
    const id = selected.value
    if (!id) {
      detail.value = null
      return
    }
    const request = new AbortController()
    detailRequest = request
    if (!quiet) detailLoading.value = true
    try {
      const next = await api.get(id, request.signal)
      if (id !== selected.value) return
      detail.value = next
      error.value = ''
      if (next.unread_count) {
        await api.read(id)
        refreshNavigation()
        void loadList(true)
      }
    } catch (e) {
      if (!request.signal.aborted) {
        error.value = workspaceError(e)
        if (!quiet) detail.value = null
      }
    } finally {
      if (!request.signal.aborted) detailLoading.value = false
    }
  }
  function select(id: number) {
    creating.value = false
    return router.replace(`${admin ? '/admin' : ''}/tickets/${id}`)
  }
  function back() {
    creating.value = false
    return router.replace(`${admin ? '/admin' : ''}/tickets`)
  }
  async function create(draft: TicketDraft) {
    busy.value = true
    try {
      const { id } = await api.create(draft)
      creating.value = false
      await select(id)
      await loadList()
      return true
    } catch (e) {
      await loadList(true)
      error.value = workspaceError(e)
      return false
    } finally {
      busy.value = false
    }
  }
  async function reply(draft: TicketDraft) {
    if (!selected.value) return false
    busy.value = true
    try {
      await api.reply(selected.value, draft)
      await Promise.all([loadDetail(true), loadList(true)])
      return true
    } catch (e) {
      error.value = workspaceError(e)
      return false
    } finally {
      busy.value = false
    }
  }
  async function changeStatus(status: TicketStatus) {
    if (!selected.value) return
    busy.value = true
    try {
      await api.status(selected.value, status)
      await Promise.all([loadDetail(true), loadList(true)])
    } catch (e) {
      error.value = workspaceError(e)
    } finally {
      busy.value = false
    }
  }
  async function deleteTicket(id: number) {
    busy.value = true
    try {
      await api.delete(id)
      if (selected.value === id) await back()
      await loadList()
      return true
    } catch (e) {
      error.value = workspaceError(e)
      return false
    } finally {
      busy.value = false
    }
  }
  async function cleanupEnded() {
    busy.value = true
    try {
      const result = await api.cleanup()
      if (selected.value) await back()
      await loadList()
      return result.deleted
    } catch (e) {
      error.value = workspaceError(e)
      return null
    } finally {
      busy.value = false
    }
  }
  watch(
    selected,
    () => {
      creating.value = false
      void loadDetail()
    },
    { immediate: true }
  )
  watch(
    () => [filters.status, filters.page],
    () => void loadList()
  )
  watch(
    () => filters.search,
    () => {
      clearTimeout(debounce)
      debounce = setTimeout(() => {
        if (filters.page !== 1) filters.page = 1
        else void loadList()
      }, 250)
    }
  )
  onMounted(() => {
    void loadList()
    timer = setInterval(() => {
      if (document.visibilityState === 'visible' && !busy.value) {
        void loadList(true)
        if (selected.value) void loadDetail(true)
      }
    }, 20000)
  })
  onBeforeUnmount(() => {
    listRequest?.abort()
    detailRequest?.abort()
    clearInterval(timer)
    clearTimeout(debounce)
  })
  return {
    api,
    filters,
    result,
    detail,
    selected,
    loading,
    detailLoading,
    busy,
    error,
    creating,
    loadList,
    loadDetail,
    select,
    back,
    create,
    reply,
    changeStatus,
    deleteTicket,
    cleanupEnded
  }
}
