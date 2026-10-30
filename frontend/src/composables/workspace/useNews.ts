import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { workspaceAPI, workspaceError, type FeedState, type NewsItem, type Taxonomy } from '@/api/workspace'
import { useNavigationStore } from '@/stores/navigation'

export function useNews() {
  const view = ref('latest'),
    search = ref(''),
    filters = reactive({ categoryId: '', tagId: '', sourceId: '', sourceType: '' })
  const items = ref<NewsItem[]>([]),
    favorites = ref<NewsItem[]>([]),
    categories = ref<Taxonomy[]>([]),
    tags = ref<Taxonomy[]>([]),
    sources = ref<Taxonomy[]>([])
  const loading = ref(false),
    error = ref(''),
    cursor = ref<string | null>(null),
    hasMore = ref(false),
    updatedAt = ref<number | null>(null),
    feedState = ref<FeedState | null>(null),
    saving = ref(new Set<number>())
  let request: AbortController | null = null,
    timer: ReturnType<typeof setInterval> | undefined,
    alive = true
  const favoriteIds = computed(() => new Set(favorites.value.map((item) => item.id)))
  const visible = computed(() => {
    const list = view.value === 'favorites' ? favorites.value : items.value
    const term = search.value.trim().toLowerCase()
    return list.filter(
      (item) =>
        (!term || `${item.title} ${item.summary}`.toLowerCase().includes(term)) &&
        (!filters.categoryId || String(item.category?.id) === filters.categoryId) &&
        (!filters.tagId || item.tags.some((tag) => String(tag.id) === filters.tagId)) &&
        (!filters.sourceId || String(item.source.id) === filters.sourceId) &&
        (!filters.sourceType || String(item.source.type) === filters.sourceType)
    )
  })
  const sourceChoice = computed({
    get: () => filters.sourceId ? `${filters.sourceType}:${filters.sourceId}` : '',
    set: (value: string) => {
      const [type = '', id = ''] = value.split(':')
      filters.sourceType = type
      filters.sourceId = id
    }
  })
  const loadedItems = computed(() => [...items.value, ...favorites.value])
  const availableCategories = computed(() => categories.value.length ? categories.value :
    [...new Map(loadedItems.value.flatMap(item => item.category ? [[item.category.id, item.category] as const] : [])).values()])
  const availableTags = computed(() => tags.value.length ? tags.value :
    [...new Map(loadedItems.value.flatMap(item => item.tags.map(tag => [tag.id, tag] as const))).values()])
  const availableSources = computed(() => sources.value.length ? sources.value :
    [...new Map(loadedItems.value.map(item => [`${item.source.type}:${item.source.id}`, item.source])).values()])
  async function load(more = false, quiet = false) {
    if (view.value === 'favorites') {
      loading.value = false
      return
    }
    request?.abort()
    const current = new AbortController()
    request = current
    loading.value = true
    try {
      const page = await workspaceAPI.news(
        view.value,
        {
          ...(!feedState.value?.stale ? Object.fromEntries(Object.entries(filters).filter(([, value]) => value)) : {}),
          limit: 30,
          cursor: more ? cursor.value || undefined : undefined
        },
        current.signal
      )
      if (current.signal.aborted) return
      void useNavigationStore().viewed('/news', page.items.map(item => String(item.id)))
      const previous = items.value
      items.value = more
        ? [...previous, ...page.items.filter((item) => !previous.some((old) => old.id === item.id))]
        : quiet
          ? [
              ...page.items,
              ...previous.filter((item) => !page.items.some((next) => next.id === item.id))
            ]
          : page.items
      if (!quiet) {
        cursor.value = page.nextCursor || null
        hasMore.value = !!page.hasMore
      }
      feedState.value = page.feedState
      updatedAt.value = page.feedState.fetchedAt
      error.value = ''
    } catch (e) {
      if (!current.signal.aborted) error.value = workspaceError(e)
    } finally {
      if (!current.signal.aborted) loading.value = false
    }
  }
  async function toggleFavorite(item: NewsItem) {
    if (saving.value.has(item.id)) return
    saving.value = new Set([...saving.value, item.id])
    try {
      if (favoriteIds.value.has(item.id)) {
        await workspaceAPI.unfavorite(item.id)
        favorites.value = favorites.value.filter((old) => old.id !== item.id)
      } else {
        await workspaceAPI.favorite(item)
        favorites.value = [{ ...item, favorited: true }, ...favorites.value].slice(0, 100)
      }
    } catch (e) {
      error.value = workspaceError(e)
    } finally {
      const next = new Set(saving.value)
      next.delete(item.id)
      saving.value = next
    }
  }
  watch(
    [
      view,
      () => filters.categoryId,
      () => filters.tagId,
      () => filters.sourceId,
      () => filters.sourceType
    ],
    (next, previous) => {
      request?.abort()
      if (next[0] !== previous[0]) {
        items.value = []
        feedState.value = null
        updatedAt.value = null
      }
      error.value = ''
      cursor.value = null
      hasMore.value = false
      void load()
    }
  )
  onMounted(async () => {
    void load()
    const results = await Promise.allSettled([
      workspaceAPI.favorites(),
      workspaceAPI.taxonomy('categories'),
      workspaceAPI.taxonomy('tags'),
      workspaceAPI.taxonomy('sources/rss'),
      workspaceAPI.taxonomy('sources/x')
    ])
    if (!alive) return
    if (results[0].status === 'fulfilled') favorites.value = results[0].value.items as NewsItem[]
    if (results[1].status === 'fulfilled') categories.value = results[1].value.items as Taxonomy[]
    if (results[2].status === 'fulfilled') tags.value = results[2].value.items as Taxonomy[]
    sources.value = results
      .slice(3)
      .flatMap((result) =>
        result.status === 'fulfilled' ? (result.value.items as Taxonomy[]) : []
      )
    timer = setInterval(() => {
      if (document.visibilityState === 'visible' && view.value === 'latest' && !loading.value)
        void load(false, true)
    }, 60000)
  })
  onBeforeUnmount(() => {
    alive = false
    request?.abort()
    clearInterval(timer)
  })
  return {
    view,
    search,
    filters,
    visible,
    categories: availableCategories,
    tags: availableTags,
    sources: availableSources,
    sourceChoice,
    favorites,
    favoriteIds,
    loading,
    error,
    hasMore,
    updatedAt,
    feedState,
    saving,
    load,
    toggleFavorite
  }
}
