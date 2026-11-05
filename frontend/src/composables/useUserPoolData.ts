import { computed, shallowRef, watch } from 'vue'
import { listUserPoolAccounts, listUserPoolUsage, type UserPoolAccount, type UserPoolUsage } from '@/api/pool'
import { listSharedPoolAccounts, listSharedPoolUsage, PoolShareUnavailableError } from '@/api/poolShare'
import type { PaginatedResponse } from '@/types'
import { usePoolRequest } from './usePoolRequest'

export type UserPoolTab = 'accounts' | 'usage'

export function useUserPoolData(options: {
  shared: () => boolean | undefined
  token: () => string | undefined
  initialTab: () => UserPoolTab | undefined
}) {
  const query = usePoolRequest<PaginatedResponse<UserPoolAccount> | null>(null)
  const usageQuery = usePoolRequest<PaginatedResponse<UserPoolUsage> | null>(null)
  const activeTab = shallowRef<UserPoolTab>(options.initialTab() ?? 'accounts')
  const page = shallowRef(1)
  const usagePage = shallowRef(1)
  const invalid = shallowRef(false)
  const currentQuery = computed(() => activeTab.value === 'usage' ? usageQuery : query)
  const loading = computed(() => currentQuery.value.loading.value)
  const failed = computed(() => currentQuery.value.failed.value)
  const updatedAt = computed(() => currentQuery.value.updatedAt.value)
  const rows = computed(() => query.data.value?.items ?? [])
  const usageRows = computed(() => usageQuery.data.value?.items ?? [])
  const total = computed(() => query.data.value?.total ?? 0)
  const currentTotal = computed(() => currentQuery.value.data.value?.total ?? 0)
  const currentPage = computed({
    get: () => activeTab.value === 'usage' ? usagePage.value : page.value,
    set: value => { if (activeTab.value === 'usage') usagePage.value = value; else page.value = value }
  })

  function clearData() {
    query.reset()
    usageQuery.reset()
  }

  async function sharedRequest<T>(token: string, signal: AbortSignal, request: () => Promise<T>): Promise<T> {
    try { return await request() }
    catch (error) {
      if (!signal.aborted && options.shared() && options.token() === token && error instanceof PoolShareUnavailableError) {
        invalid.value = true
        clearData()
      }
      throw error
    }
  }

  async function refreshCurrent(): Promise<boolean> {
    if (invalid.value) return false
    const shared = options.shared()
    const token = options.token() ?? ''
    if (shared && !token) {
      invalid.value = true
      clearData()
      return false
    }
    const current = currentPage.value
    if (activeTab.value === 'usage') {
      return usageQuery.run(signal => shared
        ? sharedRequest(token, signal, () => listSharedPoolUsage(token, current, signal))
        : listUserPoolUsage(current, signal))
    }
    return query.run(signal => shared
      ? sharedRequest(token, signal, () => listSharedPoolAccounts(token, current, signal))
      : listUserPoolAccounts(current, signal))
  }

  // A new link must never retain data, errors or pages from the previous owner.
  watch([options.shared, options.token, options.initialTab], () => {
    clearData()
    invalid.value = false
    page.value = 1
    usagePage.value = 1
    activeTab.value = options.initialTab() ?? 'accounts'
  }, { flush: 'sync' })

  watch([activeTab, currentPage, options.shared, options.token, options.initialTab], () => {
    query.cancel()
    usageQuery.cancel()
    currentQuery.value.reset()
    void refreshCurrent()
  }, { immediate: true })

  return { activeTab, invalid, loading, failed, updatedAt, rows, usageRows, total, currentTotal, currentPage, refreshCurrent }
}
