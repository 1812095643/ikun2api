import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useNavigationStore } from '@/stores/navigation'
import {
  getWorkspaceModels,
  type ModelPlazaGroup,
  type ModelPlazaResponse,
  type PlazaModel
} from '@/api/modelPlaza'
import { workspaceError } from '@/api/workspace'
import { formatScaled } from '@/utils/pricing'

export interface ModelEntry {
  name: string
  platform: string
  offers: { group: ModelPlazaGroup; model: PlazaModel }[]
}
export function modelPrice(
  entry: ModelEntry,
  key: 'input_price' | 'output_price' | 'cache_read_price' | 'cache_write_price'
): string {
  const prices = entry.offers.flatMap(({ group, model }) => {
    if ((model.pricing?.billing_mode || 'token') !== 'token') return []
    const value = model.pricing?.[key]
    return value == null ? [] : [value * (group.user_rate_multiplier ?? group.rate_multiplier)]
  })
  return prices.length ? formatScaled(Math.min(...prices), 1000000, 2) : '—'
}
export function mediaPrice(entry: ModelEntry): string {
  const prices = entry.offers.flatMap(({ group, model }) => {
    const value = model.pricing?.per_request_price
    if (value == null) return []
    const mode = model.pricing?.billing_mode
    const rate =
      mode === 'image' && group.image_rate_independent
        ? group.image_rate_multiplier
        : mode === 'video' && group.video_rate_independent
          ? group.video_rate_multiplier
          : (group.user_rate_multiplier ?? group.rate_multiplier)
    return [value * rate]
  })
  return prices.length ? formatScaled(Math.min(...prices), 1, 2) : '按档位查看'
}
export function useModelSquare() {
  const data = ref<ModelPlazaResponse | null>(null),
    loading = ref(false),
    error = ref(''),
    search = ref(''),
    platform = ref(''),
    group = ref(''),
    mode = ref('cards'),
    selected = ref<ModelEntry | null>(null)
  let request: AbortController | null = null
  const platforms = computed(() => [
    ...new Set((data.value?.groups || []).map((group) => group.platform))
  ])
  const platformCounts = computed(() => Object.fromEntries(platforms.value.map(value => [value,
    new Set((data.value?.groups || []).filter(item => item.platform === value).flatMap(item => item.models.map(model => model.name))).size
  ])))
  const totalModels = computed(() => new Set((data.value?.groups || []).flatMap(item =>
    item.models.map(model => `${model.platform || item.platform}:${model.name}`))).size)
  const groups = computed(() =>
    (data.value?.groups || []).filter(
      (group) => !platform.value || group.platform === platform.value
    )
  )
  const entries = computed<ModelEntry[]>(() => {
    const map = new Map<string, ModelEntry>(),
      query = search.value.trim().toLowerCase()
    for (const current of groups.value) {
      if (group.value && String(current.id) !== group.value) continue
      for (const model of current.models) {
        if (query && !model.name.toLowerCase().includes(query)) continue
        const key = `${model.platform}:${model.name}`
        const entry = map.get(key) || {
          name: model.name,
          platform: model.platform || current.platform,
          offers: []
        }
        entry.offers.push({ group: current, model })
        map.set(key, entry)
      }
    }
    return [...map.values()].sort(
      (a, b) => a.platform.localeCompare(b.platform) || a.name.localeCompare(b.name)
    )
  })
  const filteredGroups = computed(() =>
    groups.value
      .filter((item) => !group.value || String(item.id) === group.value)
      .map((item) => ({
        ...item,
        models: item.models.filter(
          (model) => !search.value || model.name.toLowerCase().includes(search.value.toLowerCase())
        )
      }))
      .filter((item) => item.models.length)
  )
  async function load() {
    request?.abort()
    const current = new AbortController()
    request = current
    loading.value = true
    try {
      data.value = await getWorkspaceModels({ signal: current.signal })
      if (current.signal.aborted) return
      void useNavigationStore().viewed('/model-square', [...new Set(data.value.groups.flatMap(item => item.models.map(model => `${model.platform || item.platform}:${model.name}`)))])
      error.value = ''
    } catch (e) {
      if (!current.signal.aborted) error.value = workspaceError(e)
    } finally {
      if (!current.signal.aborted) loading.value = false
    }
  }
  onMounted(load)
  onBeforeUnmount(() => request?.abort())
  return {
    data,
    loading,
    error,
    search,
    platform,
    group,
    mode,
    selected,
    platforms,
    platformCounts,
    totalModels,
    groups,
    entries,
    filteredGroups,
    load
  }
}
