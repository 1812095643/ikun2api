import { ccSwitchModelsUrl, parseCcSwitchModels } from '@/utils/ccswitchImport'
import type { GroupPlatform } from '@/types'

/** A directory read only; never starts a model generation or follows a redirect with the user's key. */
export async function fetchCcSwitchModels(baseUrl: string, apiKey: string, platform?: GroupPlatform, signal?: AbortSignal): Promise<string[]> {
  const response = await fetch(ccSwitchModelsUrl(baseUrl, platform), {
    method: 'GET',
    headers: { Accept: 'application/json', Authorization: `Bearer ${apiKey}` },
    credentials: 'omit', redirect: 'error', referrerPolicy: 'no-referrer', cache: 'no-store', signal
  })
  if (!response.ok) throw new Error('modelsUnavailable')
  const text = await response.text()
  if (text.length > 2 * 1024 * 1024) throw new Error('modelsTooLarge')
  return parseCcSwitchModels(JSON.parse(text))
}
