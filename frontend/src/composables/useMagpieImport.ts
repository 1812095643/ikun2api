import { computed, onScopeDispose, shallowRef, watch } from 'vue'
import type { ApiKey } from '@/types'
import { fetchCcSwitchModels } from '@/api/ccswitch'
import { DEFAULT_SITE_NAME } from '@/utils/branding'
import { buildMagpieImportDeeplink, defaultMagpieFormat, getMagpieFormats, resolveMagpieEndpoint } from '@/utils/magpieImport'
import type { CcSwitchApiFormat } from '@/utils/ccswitchImport'

export function useMagpieImport(options: { apiKey: () => ApiKey | null; baseUrl: () => string; siteName: () => string }) {
  const group = computed(() => options.apiKey()?.group ?? null)
  const formats = computed(() => getMagpieFormats(group.value))
  const format = shallowRef<CcSwitchApiFormat>('anthropic')
  const providerName = shallowRef('')
  const model = shallowRef('')
  const models = shallowRef<string[]>([])
  const modelsState = shallowRef<'idle' | 'loading' | 'ready' | 'error'>('idle')
  const opened = shallowRef(false)
  const endpoint = computed(() => {
    try { return resolveMagpieEndpoint(options.baseUrl(), format.value, group.value) }
    catch { return '' }
  })
  let controller: AbortController | null = null
  let generation = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  function cancelModels() {
    generation++
    controller?.abort()
    controller = null
    clearTimeout(timer)
  }
  async function refreshModels() {
    cancelModels()
    const row = options.apiKey()
    if (!row || !formats.value.length || !endpoint.value) return
    const currentGeneration = generation
    const request = new AbortController()
    controller = request
    modelsState.value = 'loading'
    timer = setTimeout(() => request.abort(), 15000)
    try {
      const modelPlatform = (row.group_ids?.length ?? 0) > 1 ? undefined : group.value?.platform
      const result = await fetchCcSwitchModels(options.baseUrl(), row.key, modelPlatform, request.signal)
      if (currentGeneration !== generation) return
      models.value = result
      modelsState.value = 'ready'
    } catch {
      if (currentGeneration === generation) modelsState.value = 'error'
    } finally {
      if (currentGeneration === generation) { clearTimeout(timer); controller = null }
    }
  }
  watch(() => [options.apiKey(), options.baseUrl(), options.siteName()] as const, () => {
    cancelModels()
    models.value = []; modelsState.value = 'idle'; model.value = ''; opened.value = false
    format.value = defaultMagpieFormat(group.value)
    // Respect Magpie's 80 UTF-8 byte limit without splitting Unicode characters.
    let name = ''
    for (const character of [options.siteName().trim() || DEFAULT_SITE_NAME, options.apiKey()?.name].filter(Boolean).join(' · ')) {
      if (new TextEncoder().encode(name + character).length > 80) break
      name += character
    }
    providerName.value = name
    if (options.apiKey()) void refreshModels()
  }, { immediate: true })
  watch(format, () => { opened.value = false })
  onScopeDispose(cancelModels)
  const importResult = computed(() => {
    const row = options.apiKey()
    if (!row) return { url: '', error: 'unavailable' }
    try {
      return { url: buildMagpieImportDeeplink({
        baseUrl: options.baseUrl(), group: group.value, format: format.value,
        providerName: providerName.value, apiKey: row.key, keyId: row.id, model: model.value
      }), error: '' }
    } catch (error) {
      const code = error instanceof Error ? error.message : ''
      return { url: '', error: ['unavailable', 'httpsRequired', 'invalidEndpoint', 'invalidName', 'invalidKey', 'invalidModel'].includes(code) ? code : 'invalidEndpoint' }
    }
  })
  return { group, formats, format, providerName, model, models, modelsState, opened, endpoint, importResult, refreshModels }
}
