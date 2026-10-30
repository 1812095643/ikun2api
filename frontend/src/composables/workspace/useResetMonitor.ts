import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { workspaceAPI, workspaceError, type FeedState, type ResetPost, type ResetSummary } from '@/api/workspace'
import { useNavigationStore } from '@/stores/navigation'

export function useResetMonitor() {
  const summary = ref<ResetSummary | null>(null),
    items = ref<ResetPost[]>([]),
    category = ref(0),
    counts = ref<{ category: number; count: number }[]>([])
  const loading = ref(false),
    error = ref(''),
    cursor = ref<string | null>(null),
    feedState = ref<FeedState | null>(null),
    now = ref(Date.now())
  let request: AbortController | null = null,
    timer: ReturnType<typeof setInterval> | undefined,
    clock: ReturnType<typeof setInterval> | undefined
  const elapsed = computed(() => {
    if (!summary.value?.lastResetAt) return null
    const delta = Math.max(0, now.value - new Date(summary.value.lastResetAt).getTime())
    return {
      days: Math.floor(delta / 86400000),
      hours: Math.floor(delta / 3600000) % 24,
      minutes: Math.floor(delta / 60000) % 60,
      seconds: Math.floor(delta / 1000) % 60
    }
  })
  async function load(more = false) {
    request?.abort()
    const current = new AbortController()
    request = current
    loading.value = true
    try {
      const [feedResult, summaryResult] = await Promise.allSettled([
        workspaceAPI.resetPosts(
          {
            category: feedState.value?.stale ? undefined : category.value || undefined,
            limit: 20,
            cursor: more ? cursor.value || undefined : undefined
          },
          current.signal
        ),
        more ? Promise.resolve(null) : workspaceAPI.resetSummary(current.signal)
      ])
      if (current.signal.aborted) return
      const states: FeedState[] = []
      const failures: string[] = []
      if (summaryResult.status === 'fulfilled' && summaryResult.value) {
        summary.value = summaryResult.value
        states.push(summaryResult.value.feedState)
      } else if (summaryResult.status === 'rejected') {
        failures.push(workspaceError(summaryResult.reason))
      }
      if (feedResult.status === 'fulfilled') {
        const feed = feedResult.value
        void useNavigationStore().viewed('/reset-monitor', feed.items.map(item => String(item.id)))
        states.push(feed.feedState)
        items.value = more
        ? [
            ...items.value,
            ...feed.items.filter((post) => !items.value.some((old) => old.id === post.id))
          ]
        : feed.items
        counts.value = feed.counts || []
        cursor.value = feed.nextCursor || null
      } else {
        failures.push(workspaceError(feedResult.reason))
      }
      const timestamps = states.flatMap(state => state.fetchedAt ? [state.fetchedAt] : [])
      if (states.length) feedState.value = {
        stale: states.some(state => state.stale),
        fetchedAt: timestamps.length ? Math.min(...timestamps) : null
      }
      error.value = [...new Set(failures)].join(' ')
    } catch (e) {
      if (!current.signal.aborted) error.value = workspaceError(e)
    } finally {
      if (!current.signal.aborted) loading.value = false
    }
  }
  watch(category, () => {
    cursor.value = null
    void load()
  })
  onMounted(() => {
    void load()
    clock = setInterval(() => {
      now.value = Date.now()
    }, 60000)
    timer = setInterval(() => {
      if (document.visibilityState === 'visible' && !loading.value && items.value.length <= 20)
        void load()
    }, 60000)
  })
  onBeforeUnmount(() => {
    request?.abort()
    clearInterval(timer)
    clearInterval(clock)
  })
  const visible = computed(() => items.value.filter(item => !category.value || item.category === category.value))
  return { summary, items: visible, category, counts, loading, error, cursor, elapsed, now, feedState, load }
}
