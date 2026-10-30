import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  reloginAPI,
  type ImportedPage,
  type LoginRun,
  type ReloginAccount,
  type ReloginOptions,
  type ReloginState
} from '@/api/relogin'
import { workspaceError } from '@/api/workspace'

export function useRelogin() {
  const tab = ref('accounts'),
    accounts = ref<ReloginAccount[]>([]),
    imported = ref<ImportedPage | null>(null),
    history = ref<LoginRun[]>([]),
    settings = ref<ReloginState | null>(null),
    options = ref<ReloginOptions>({ groups: [], proxies: [] })
  const loading = ref(false),
    busy = ref(false),
    error = ref(''),
    notice = ref(''),
    search = ref(''),
    status = ref(''),
    importedQuery = ref(''),
    importedPage = ref(1)
  let alive = true,
    timer: ReturnType<typeof setInterval> | undefined,
    debounce: ReturnType<typeof setTimeout> | undefined
  const activeRuns = computed(() =>
    history.value.filter((run) => run.status === 'running' || run.status === 'challenge')
  )
  const visibleAccounts = computed(() =>
    accounts.value.filter(
      (account) =>
        (!status.value || account.status === status.value) &&
        (!search.value ||
          `${account.name} ${account.email}`.toLowerCase().includes(search.value.toLowerCase()))
    )
  )
  async function loadImported() {
    try {
      const value = await reloginAPI.imported({
        page: importedPage.value,
        query: importedQuery.value
      })
      if (alive) imported.value = value
    } catch (e) {
      if (alive) error.value = workspaceError(e)
    }
  }
  async function refresh(quiet = false) {
    if (!quiet) loading.value = true
    try {
      const result = await Promise.all([
        reloginAPI.accounts(),
        reloginAPI.history(),
        reloginAPI.settings()
      ])
      if (!alive) return
      accounts.value = result[0]
      history.value = result[1]
      settings.value = result[2]
      if (!quiet) {
        await loadImported()
        options.value = await reloginAPI.options()
        error.value = ''
      }
    } catch (e) {
      if (alive) error.value = workspaceError(e)
    } finally {
      if (alive) loading.value = false
    }
  }
  async function act(action: () => Promise<unknown>, message: string) {
    if (busy.value) return false
    busy.value = true
    error.value = ''
    notice.value = ''
    try {
      await action()
      notice.value = message
      await refresh()
      return true
    } catch (e) {
      error.value = workspaceError(e)
      return false
    } finally {
      busy.value = false
    }
  }
  watch(importedPage, loadImported)
  watch(importedQuery, () => {
    clearTimeout(debounce)
    debounce = setTimeout(() => {
      importedPage.value = 1
      void loadImported()
    }, 250)
  })
  onMounted(() => {
    void refresh()
    timer = setInterval(() => {
      if (
        document.visibilityState === 'visible' &&
        activeRuns.value.some((run) => run.status === 'running') &&
        !loading.value
      )
        void refresh(true)
    }, 4000)
  })
  onBeforeUnmount(() => {
    alive = false
    clearInterval(timer)
    clearTimeout(debounce)
  })
  return {
    tab,
    accounts,
    imported,
    history,
    settings,
    options,
    loading,
    busy,
    error,
    notice,
    search,
    status,
    importedQuery,
    importedPage,
    activeRuns,
    visibleAccounts,
    loadImported,
    refresh,
    act
  }
}
