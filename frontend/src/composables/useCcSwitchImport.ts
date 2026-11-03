import { computed, onScopeDispose, ref, shallowRef, watch } from 'vue'
import type { ApiKey } from '@/types'
import { fetchCcSwitchModels } from '@/api/ccswitch'
import {
  buildCcSwitchImportDeeplink, ccSwitchAllowsAutomaticModel, ccSwitchNeedsRouting,
  defaultCcSwitchClient, defaultCcSwitchFormat, getCcSwitchClients, getCcSwitchFormats,
  normalizeCcSwitchBaseUrl, resolveCcSwitchEndpoint,
  type CcSwitchApiFormat, type CcSwitchClientType
} from '@/utils/ccswitchImport'

export function useCcSwitchImport(options: {
  apiKey: () => ApiKey | null
  baseUrl: () => string
  siteName: () => string
}) {
  const group = computed(() => options.apiKey()?.group ?? null)
  const clients = computed(() => getCcSwitchClients(group.value))
  const client = shallowRef<CcSwitchClientType>('claude')
  const format = shallowRef<CcSwitchApiFormat>('anthropic')
  const formats = computed(() => getCcSwitchFormats(client.value, group.value))
  const providerName = shallowRef('')
  const model = shallowRef('')
  const haikuModel = shallowRef('')
  const sonnetModel = shallowRef('')
  const opusModel = shallowRef('')
  const models = ref<string[]>([])
  const modelsState = shallowRef<'idle' | 'loading' | 'ready' | 'error'>('idle')
  const usageEnabled = shallowRef(true)
  const disableTelemetry = shallowRef(true)
  const opened = shallowRef(false)
  const automaticModel = computed(() => ccSwitchAllowsAutomaticModel(client.value, format.value, group.value?.platform))
  const isClaude = computed(() => client.value === 'claude' || client.value === 'claude-desktop')
  const needsRouting = computed(() => ccSwitchNeedsRouting(client.value, format.value))
  const endpoint = computed(() => {
    try { return resolveCcSwitchEndpoint(options.baseUrl(), client.value, format.value, group.value?.platform) }
    catch { return '' }
  })
  const usageAvailable = computed(() => {
    try { return normalizeCcSwitchBaseUrl(options.baseUrl()).startsWith('https://') }
    catch { return false }
  })
  let controller: AbortController | null = null
  let requestId = 0
  let timer: ReturnType<typeof setTimeout> | undefined

  function cancelModels() {
    requestId++
    controller?.abort()
    controller = null
    clearTimeout(timer)
  }
  function setDefaultModel() {
    if (automaticModel.value) { model.value = ''; return }
    const preferred = group.value?.default_mapped_model
    model.value = (preferred && models.value.includes(preferred) ? preferred : models.value.length === 1 ? models.value[0] : '') || ''
  }
  async function refreshModels() {
    cancelModels()
    const row = options.apiKey()
    if (!row || !group.value || !clients.value.length || !endpoint.value) return
    const currentId = requestId
    const currentController = new AbortController()
    controller = currentController
    modelsState.value = 'loading'
    timer = setTimeout(() => currentController.abort(), 15000)
    try {
      const result = await fetchCcSwitchModels(options.baseUrl(), row.key, group.value.platform, currentController.signal)
      if (currentId !== requestId) return
      models.value = result
      modelsState.value = 'ready'
      if (!model.value && !automaticModel.value) setDefaultModel()
    } catch {
      if (currentId === requestId) modelsState.value = 'error'
    } finally {
      if (currentId === requestId) { clearTimeout(timer); controller = null }
    }
  }
  watch(client, () => {
    format.value = defaultCcSwitchFormat(client.value, group.value)
    opened.value = false
    haikuModel.value = ''; sonnetModel.value = ''; opusModel.value = ''
    setDefaultModel()
  })
  watch(format, () => { opened.value = false; setDefaultModel() })
  watch(() => [options.apiKey(), options.baseUrl(), options.siteName()] as const, () => {
    cancelModels()
    const row = options.apiKey()
    models.value = []; modelsState.value = 'idle'; opened.value = false
    model.value = ''; haikuModel.value = ''; sonnetModel.value = ''; opusModel.value = ''
    client.value = defaultCcSwitchClient(group.value)
    format.value = defaultCcSwitchFormat(client.value, group.value)
    providerName.value = [options.siteName().trim() || 'IKUN2api', row?.name].filter(Boolean).join(' · ').slice(0, 120)
    usageEnabled.value = true; disableTelemetry.value = true
    if (row) void refreshModels()
  }, { immediate: true })
  onScopeDispose(cancelModels)

  const importResult = computed(() => {
    if (!clients.value.length || !formats.value.includes(format.value)) return { url: '', error: 'unavailable' }
    const row = options.apiKey()
    if (!row) return { url: '', error: 'unavailable' }
    try {
      return { url: buildCcSwitchImportDeeplink({
        baseUrl: options.baseUrl(), platform: group.value?.platform,
        clientType: client.value, apiFormat: format.value, providerName: providerName.value,
        apiKey: row.key, model: model.value, haikuModel: haikuModel.value,
        sonnetModel: sonnetModel.value, opusModel: opusModel.value,
        usageEnabled: usageEnabled.value && usageAvailable.value, disableTelemetry: disableTelemetry.value
      }), error: '' }
    } catch (error) {
      const code = error instanceof Error ? error.message : 'invalidEndpoint'
      return { url: '', error: ['invalidEndpoint', 'invalidName', 'invalidKey', 'invalidModel', 'invalidFormat', 'dynamicValue'].includes(code) ? code : 'invalidEndpoint' }
    }
  })
  return {
    group, clients, client, formats, format, providerName, model, models, modelsState,
    haikuModel, sonnetModel, opusModel, usageEnabled, usageAvailable, disableTelemetry,
    automaticModel, isClaude, needsRouting, endpoint, importResult, opened, refreshModels
  }
}
