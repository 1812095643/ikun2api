import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { BillingMode } from '@/constants/channel'
import { useNavigationStore } from '@/stores/navigation'
import {
  getWorkspaceModels,
  type ModelPlazaGroup,
  type ModelPlazaResponse,
  type PlazaModel
} from '@/api/modelPlaza'
import { workspaceError } from '@/api/workspace'
import { formatScaled } from '@/utils/pricing'
import { modelPlazaOriginalPricing, modelPlazaReferencePricing, modelPlazaPaidPrice, type ModelPlazaCurrency, type ModelPlazaTokenPriceKey } from '@/utils/modelPlazaPricing'

export interface ModelEntry {
  name: string
  platform: string
  offers: { group: ModelPlazaGroup; model: PlazaModel }[]
}

function originalTokenPricing(entry: ModelEntry): PlazaModel['official_pricing'] {
  for (const { model } of entry.offers) {
    const pricing = modelPlazaOriginalPricing({ ...model, name: entry.name })
    if (pricing) return pricing
  }
  return null
}

export function modelPrice(
  entry: ModelEntry,
  key: ModelPlazaTokenPriceKey,
  currency: ModelPlazaCurrency = 'usd',
  creditsPerCny = 0
): string {
  const original = originalTokenPricing(entry)
  const prices = entry.offers.flatMap(({ group, model }) => {
    if ((model.pricing?.billing_mode || 'token') !== 'token') return []
    const value = (original ?? model.pricing)?.[key]
    return value == null || !Number.isFinite(value) || value < 0 ? [] : [value * (group.user_rate_multiplier ?? group.rate_multiplier)]
  })
  return prices.length ? modelPlazaPaidPrice(Math.min(...prices), 1000000, currency, creditsPerCny) : '—'
}

export function modelOfficialPrice(entry: ModelEntry, key: ModelPlazaTokenPriceKey): string {
  const original = originalTokenPricing(entry)
  const prices = entry.offers.flatMap(({ model }) => {
    if ((model.pricing?.billing_mode || 'token') !== 'token') return []
    const reference = original ?? modelPlazaReferencePricing({ ...model, name: entry.name })
    const value = reference?.[key]
    return value != null && Number.isFinite(value) && value >= 0 ? [value] : []
  })
  return prices.length ? formatScaled(Math.min(...prices), 1000000, 2) : '—'
}
type MediaBillingMode = Exclude<BillingMode, 'token'>

const mediaBillingModes: MediaBillingMode[] = ['image', 'video', 'per_request']

export function mediaPrice(
  entry: ModelEntry,
  currency: ModelPlazaCurrency = 'usd',
  creditsPerCny = 0,
  billingMode?: MediaBillingMode
): string {
  const modes = mediaBillingModes.filter(mode => entry.offers.some(offer => offer.model.pricing?.billing_mode === mode))
  // A dollar per second cannot be compared with a dollar per request.
  if (!billingMode && modes.length > 1) return '按分组查看'
  const selectedMode = billingMode ?? modes[0]
  const prices = entry.offers.flatMap(({ group, model }) => {
    const pricing = model.pricing
    if (!pricing || pricing.billing_mode !== selectedMode) return []
    const tierPrices = (pricing.intervals ?? []).flatMap(interval =>
      interval.per_request_price != null && Number.isFinite(interval.per_request_price) && interval.per_request_price >= 0
        ? [interval.per_request_price] : [])
    const values = tierPrices.length ? tierPrices : [pricing.per_request_price]
    const rate = selectedMode === 'image' && group.image_rate_independent
      ? group.image_rate_multiplier
      : selectedMode === 'video' && group.video_rate_independent
        ? group.video_rate_multiplier
        : (group.user_rate_multiplier ?? group.rate_multiplier)
    if (!Number.isFinite(rate)) return []
    return values.flatMap(value => value != null && Number.isFinite(value) && value >= 0
      ? [value * Math.max(0, rate)] : [])
  })
  return prices.length ? modelPlazaPaidPrice(Math.min(...prices), 1, currency, creditsPerCny) : '按档位查看'
}

export function mediaPriceRows(entry: ModelEntry, currency: ModelPlazaCurrency = 'usd', creditsPerCny = 0) {
  return mediaBillingModes
    .filter(mode => entry.offers.some(offer => offer.model.pricing?.billing_mode === mode))
    .map(mode => ({ mode, price: mediaPrice(entry, currency, creditsPerCny, mode) }))
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
