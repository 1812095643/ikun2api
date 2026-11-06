import {
  defaultCcSwitchFormat, getCcSwitchFormats, normalizeCcSwitchBaseUrl, validCcSwitchModel,
  type CcSwitchApiFormat, type CcSwitchGroup
} from '@/utils/ccswitchImport'

// Matches yetone/magpie internal/provider/importurl.go; import links add a provider,
// not an agent configuration. Never put the gateway key into a vendor preset.
export function getMagpieFormats(group: CcSwitchGroup | null): CcSwitchApiFormat[] {
  return getCcSwitchFormats('opencode', group)
}

export function defaultMagpieFormat(group: CcSwitchGroup | null): CcSwitchApiFormat {
  return defaultCcSwitchFormat('opencode', group)
}

export function resolveMagpieEndpoint(baseUrl: string, format: CcSwitchApiFormat, group: CcSwitchGroup | null): string {
  let root = normalizeCcSwitchBaseUrl(baseUrl)
  // The official parser permits HTTP only on loopback or private network hosts.
  if (new URL(root).protocol !== 'https:') throw new Error('httpsRequired')
  if (!getMagpieFormats(group).includes(format)) throw new Error('unavailable')
  if (group?.platform === 'antigravity' && ['anthropic', 'gemini_native'].includes(format) && !root.endsWith('/antigravity')) root += '/antigravity'
  if (format === 'gemini_native') return `${root}/v1beta`
  if (format === 'openai_chat' || format === 'openai_responses') return `${root}/v1`
  return root
}

export function buildMagpieImportDeeplink(input: {
  baseUrl: string
  group: CcSwitchGroup | null
  format: CcSwitchApiFormat
  providerName: string
  apiKey: string
  keyId: number
  model?: string
}): string {
  const endpoint = resolveMagpieEndpoint(input.baseUrl, input.format, input.group)
  const name = input.providerName.trim()
  const hasControl = (value: string) => Array.from(value).some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127)
  if (!name || new TextEncoder().encode(name).length > 80 || hasControl(name)) throw new Error('invalidName')
  if (!input.apiKey || new TextEncoder().encode(input.apiKey).length > 4096 || /\s/u.test(input.apiKey) || hasControl(input.apiKey) || !Number.isSafeInteger(input.keyId) || input.keyId <= 0) throw new Error('invalidKey')
  const model = input.model?.trim() || ''
  if (model && !validCcSwitchModel(model)) throw new Error('invalidModel')
  const root = normalizeCcSwitchBaseUrl(input.baseUrl)
  // Explicit ASCII id keeps Chinese names importable and separates keys/formats.
  const siteId = root.replace(/^https:\/\//, '').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/-+$/, '')
  const protocol = { anthropic: 'anthropic', openai_chat: 'chat', openai_responses: 'responses', gemini_native: 'gemini' }[input.format]
  const params = new URLSearchParams({
    id: `ikun-${siteId}-${input.keyId}-${protocol}`, name, key: input.apiKey, [protocol]: endpoint
  })
  if (model) params.set('models', model)
  return `magpie://import?${params.toString()}`
}
