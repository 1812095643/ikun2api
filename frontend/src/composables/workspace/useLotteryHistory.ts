import { computed, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { lotteryAPI, type LotteryHistoryFilter, type LotteryParticipationPage } from '@/api/lottery'
import { workspaceError } from '@/api/workspace'

export function useLotteryHistory(options: {
  manage: boolean
  lotteryId?: number
  revision?: number
}) {
  const data = shallowRef<LotteryParticipationPage | null>(null)
  const loading = shallowRef(false)
  const error = shallowRef('')
  const page = shallowRef(1)
  const result = ref<LotteryHistoryFilter>('')
  const activity = ref('')
  const search = ref('')
  const appliedActivity = shallowRef('')
  const appliedSearch = shallowRef('')
  let request: AbortController | undefined
  const pages = computed(() => Math.max(1, Math.ceil((data.value?.total || 0) / 20)))
  async function load() {
    request?.abort()
    const current = new AbortController()
    request = current
    loading.value = true
    error.value = ''
    try {
      const response = await lotteryAPI.participations(
        options.manage,
        {
          page: page.value,
          lottery_id: options.lotteryId,
          activity: appliedActivity.value || undefined,
          search: options.manage ? appliedSearch.value || undefined : undefined,
          result: result.value || undefined
        },
        current.signal
      )
      if (!current.signal.aborted) data.value = response
    } catch (e) {
      if (!current.signal.aborted) error.value = workspaceError(e)
    } finally {
      if (!current.signal.aborted) loading.value = false
    }
  }
  function apply() {
    page.value = 1
    appliedActivity.value = activity.value.trim()
    appliedSearch.value = search.value.trim()
    void load()
  }
  function changePage(delta: number) {
    page.value += delta
    void load()
  }
  watch(result, () => {
    page.value = 1
    void load()
  })
  watch(
    () => options.lotteryId,
    () => {
      page.value = 1
      void load()
    },
    { immediate: true }
  )
  watch(
    () => options.revision,
    () => {
      void load()
    }
  )
  onBeforeUnmount(() => request?.abort())
  return { data, loading, error, page, pages, result, activity, search, load, apply, changePage }
}
