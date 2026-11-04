import type { Group, GroupPlatform } from '@/types'
import { CLAUDE_PRIVACY_ENV } from '@/utils/claudeClientPrivacy'

// Verified against CC Switch v4.0.25 parser.rs/provider.rs and its site integration guide.
export const CC_SWITCH_VERSION = '4.0.25'
export type CcSwitchClientType = 'claude' | 'claude-desktop' | 'codex' | 'opencode' | 'pi' | 'gemini' | 'grokbuild' | 'openclaw' | 'hermes' | 'agy'
export type CcSwitchApiFormat = 'anthropic' | 'openai_chat' | 'openai_responses' | 'gemini_native'
export type CcSwitchGroup = Pick<Group, 'platform' | 'claude_code_only' | 'allow_claude_desktop' | 'allow_messages_dispatch' | 'default_mapped_model'>

export const CC_SWITCH_CLIENTS: { value: CcSwitchClientType; label: string }[] = [
  { value: 'claude', label: 'Claude Code CLI' },
  { value: 'claude-desktop', label: 'Claude Desktop' },
  { value: 'codex', label: 'Codex' },
  { value: 'opencode', label: 'OpenCode' },
  { value: 'pi', label: 'Pi' },
  { value: 'gemini', label: 'Gemini CLI' },
  { value: 'grokbuild', label: 'Grok Build' },
  { value: 'openclaw', label: 'OpenClaw' },
  { value: 'hermes', label: 'Hermes' },
  { value: 'agy', label: 'Antigravity CLI' }
]
export const CC_SWITCH_FORMATS: { value: CcSwitchApiFormat; label: string }[] = [
  { value: 'anthropic', label: 'Anthropic Messages' },
  { value: 'openai_responses', label: 'OpenAI Responses' },
  { value: 'openai_chat', label: 'OpenAI Chat Completions' },
  { value: 'gemini_native', label: 'Gemini Native' }
]

export function getCcSwitchFormats(client: CcSwitchClientType, group: CcSwitchGroup | null): CcSwitchApiFormat[] {
  if (!group || group.platform === 'typesafe') return []
  if (group.platform === 'openai_bps') return client === 'codex' ? ['openai_responses'] : []
  if (group.claude_code_only) {
    if (client !== 'claude' && !(client === 'claude-desktop' && group.allow_claude_desktop)) return []
    if (group.platform === 'openai' && !group.allow_messages_dispatch) return []
    return ['anthropic']
  }
  const formats: CcSwitchApiFormat[] = ['anthropic', 'openai_responses', 'openai_chat']
  if (group.platform === 'openai' && !group.allow_messages_dispatch) formats.splice(0, 1)
  if (['gemini', 'antigravity', 'composite'].includes(group.platform)) formats.push('gemini_native')
  if (client === 'gemini' || client === 'agy') return formats.filter(f => f === 'gemini_native')
  if (client === 'codex' || client === 'grokbuild' || client === 'hermes') return formats.filter(f => f !== 'gemini_native')
  return formats
}

export function getCcSwitchClients(group: CcSwitchGroup | null) {
  return CC_SWITCH_CLIENTS.filter(client => getCcSwitchFormats(client.value, group).length > 0)
}

export function defaultCcSwitchClient(group: CcSwitchGroup | null): CcSwitchClientType {
  if (group?.claude_code_only) return 'claude'
  const preferred: Partial<Record<GroupPlatform, CcSwitchClientType>> = {
    openai: 'codex', openai_bps: 'codex', gemini: 'gemini', grok: 'grokbuild'
  }
  return (group && preferred[group.platform]) || getCcSwitchClients(group)[0]?.value || 'claude'
}

export function defaultCcSwitchFormat(client: CcSwitchClientType, group: CcSwitchGroup | null): CcSwitchApiFormat {
  const formats = getCcSwitchFormats(client, group)
  const preferred = (client === 'claude' || client === 'claude-desktop') && formats.includes('anthropic') ? 'anthropic'
    : client === 'codex' || client === 'grokbuild' ? 'openai_responses'
    : client === 'gemini' || client === 'agy' ? 'gemini_native'
      : group?.platform === 'openai' || group?.platform === 'openai_bps' || group?.platform === 'grok' ? 'openai_responses'
        : group?.platform === 'gemini' ? 'gemini_native' : 'anthropic'
  return formats.includes(preferred) ? preferred : formats[0] || 'anthropic'
}

export function ccSwitchNeedsRouting(client: CcSwitchClientType, format: CcSwitchApiFormat): boolean {
  return ((client === 'claude' || client === 'claude-desktop') && format !== 'anthropic') ||
    ((client === 'codex' || client === 'grokbuild') && format !== 'openai_responses')
}

export function ccSwitchAllowsAutomaticModel(client: CcSwitchClientType, format: CcSwitchApiFormat, platform?: GroupPlatform): boolean {
  return ((client === 'claude' || client === 'claude-desktop') && format === 'anthropic' && platform === 'anthropic') ||
    ((client === 'gemini' || client === 'agy') && format === 'gemini_native')
}

export function normalizeCcSwitchBaseUrl(baseUrl: string): string {
  const url = new URL(baseUrl.trim())
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash || url.href.includes(',')) {
    throw new Error('invalidEndpoint')
  }
  return url.href.replace(/\/+$/, '').replace(/\/v1(?:beta)?$/i, '')
}

export function resolveCcSwitchEndpoint(baseUrl: string, client: CcSwitchClientType, format: CcSwitchApiFormat, platform?: GroupPlatform): string {
  let root = normalizeCcSwitchBaseUrl(baseUrl)
  // Keep Antigravity's forced-platform routes for its native protocols only.
  if (platform === 'antigravity' && (format === 'anthropic' || format === 'gemini_native') && !root.endsWith('/antigravity')) root += '/antigravity'
  if (format === 'openai_chat' || format === 'openai_responses') return `${root}/v1`
  if (client === 'opencode') return `${root}/${format === 'gemini_native' ? 'v1beta' : 'v1'}`
  return root
}

export function ccSwitchModelsUrl(baseUrl: string, platform?: GroupPlatform): string {
  let root = normalizeCcSwitchBaseUrl(baseUrl)
  if (platform === 'antigravity' && !root.endsWith('/antigravity')) root += '/antigravity'
  return `${root}/v1/models`
}

function hasControlCharacters(value: string): boolean {
  return Array.from(value).some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127)
}

export function validCcSwitchModel(value: string): boolean {
  return value.length > 0 && value.length <= 200 && !hasControlCharacters(value) && !/[\s"'`\\{},$?!#]/u.test(value)
}

export function parseCcSwitchModels(payload: unknown): string[] {
  if (!payload || typeof payload !== 'object') throw new Error('invalidModels')
  const object = payload as Record<string, unknown>
  const rows = Array.isArray(object.data) ? object.data : object.models
  if (!Array.isArray(rows)) throw new Error('invalidModels')
  const ids = rows.flatMap(row => {
    if (!row || typeof row !== 'object') return []
    const item = row as Record<string, unknown>
    const id = item.id ?? item.slug ?? item.name
    if (typeof id !== 'string') return []
    const model = id.replace(/^models\//, '')
    // Coding clients need conversation models, not media/embedding-only endpoints.
    if (!validCcSwitchModel(model) || /(?:image|imagine|embedding|whisper|tts|realtime|audio|sora|veo|moderation)/i.test(model)) return []
    return [model]
  })
  return [...new Set(ids)].slice(0, 2000)
}

// CC Switch evaluates this in its restricted usage-script runtime. usageBaseUrl
// is explicitly the gateway root, independent of the target client's SDK path.
export const CC_SWITCH_USAGE_SCRIPT = `({
  request: {
    url: "{{baseUrl}}".replace(/\\/+$/, "").replace(/\\/v1$/, "") + "/v1/usage",
    method: "GET",
    headers: { "Authorization": "Bearer {{apiKey}}" }
  },
  extractor: function(response) {
    var quota = response.quota || {};
    var result = { isValid: response.isValid !== false && response.is_active !== false, unit: response.unit || quota.unit || "USD" };
    if (typeof response.planName === "string") result.planName = response.planName;
    var remaining = response.remaining;
    if (typeof remaining !== "number") remaining = quota.remaining;
    if (typeof remaining !== "number") remaining = response.balance;
    if (typeof remaining === "number" && remaining >= 0) result.remaining = remaining;
    if (quota.limit > 0) { result.used = quota.used; result.total = quota.limit; }
    if (!result.isValid) result.invalidMessage = String(response.status || "Unavailable");
    return result;
  }
})`

export interface CcSwitchImportDeeplinkInput {
  baseUrl: string
  platform?: GroupPlatform | null
  clientType: CcSwitchClientType
  apiFormat: CcSwitchApiFormat
  providerName: string
  apiKey: string
  model?: string
  haikuModel?: string
  sonnetModel?: string
  opusModel?: string
  usageEnabled?: boolean
  usageScript?: string
  usageAutoInterval?: number
  disableTelemetry?: boolean
}

export function encodeCcSwitchBase64(value: string): string {
  return btoa(Array.from(new TextEncoder().encode(value), byte => String.fromCharCode(byte)).join(''))
}

export function buildCcSwitchImportDeeplink(input: CcSwitchImportDeeplinkInput): string {
  const app = input.clientType === 'claude-desktop' ? 'claude' : input.clientType
  const platform = input.platform || undefined
  const endpoint = resolveCcSwitchEndpoint(input.baseUrl, input.clientType, input.apiFormat, platform)
  const root = normalizeCcSwitchBaseUrl(input.baseUrl).replace(/\/antigravity$/, '')
  const name = input.providerName.trim()
  if (!name || name.length > 120 || hasControlCharacters(name)) throw new Error('invalidName')
  if (!input.apiKey || (hasControlCharacters(input.apiKey) || /\s/.test(input.apiKey))) throw new Error('invalidKey')
  const model = input.model?.trim() || ''
  if ((!model && !ccSwitchAllowsAutomaticModel(input.clientType, input.apiFormat, platform)) || (model && !validCcSwitchModel(model))) throw new Error('invalidModel')
  if ((app === 'gemini' || app === 'agy') && input.apiFormat !== 'gemini_native') throw new Error('invalidFormat')
  if (['codex', 'grokbuild', 'hermes'].includes(app) && input.apiFormat === 'gemini_native') throw new Error('invalidFormat')
  // Match CCS rejection of config strings that additive clients would execute/expand.
  if (['pi', 'opencode', 'openclaw', 'hermes'].includes(app)) {
    for (const value of [name, endpoint, input.apiKey, model]) {
      if (/\$\{|\{env:|\{file:/.test(value) || (app === 'pi' && /\$[A-Za-z_]/.test(value))) throw new Error('dynamicValue')
    }
    if (app === 'pi' && input.apiKey.startsWith('!')) throw new Error('dynamicValue')
  }
  const entries: [string, string][] = [
    ['resource', 'provider'], ['app', app], ['name', name], ['homepage', root],
    ['endpoint', endpoint], ['apiKey', input.apiKey], ['apiFormat', input.apiFormat]
  ]
  if (model) entries.push(['model', model])
  if (app === 'claude') {
    for (const key of ['haikuModel', 'sonnetModel', 'opusModel'] as const) {
      // Non-native models need all three aliases to avoid falling back to an unrelated Claude model.
      const value = input[key]?.trim() || (model && !/^(?:anthropic\/)?claude-/.test(model) || input.apiFormat !== 'anthropic' ? model : '')
      if (value) {
        if (!validCcSwitchModel(value)) throw new Error('invalidModel')
        entries.push([key, value])
      }
    }
    if (input.disableTelemetry !== false) {
      entries.push(['configFormat', 'json'], ['config', encodeCcSwitchBase64(JSON.stringify({ env: CLAUDE_PRIVACY_ENV }))])
    }
    if (input.clientType === 'claude-desktop') {
      entries.push(['notes', 'Claude Desktop: import this provider from Claude Code on the Claude Desktop page in CC Switch, then switch and restart Claude Desktop.'])
    }
  }
  if (app === 'codex') {
    const config = [
      'model_provider = "ikun"', `model = ${JSON.stringify(model)}`, 'disable_response_storage = true', '',
      '[model_providers.ikun]', `name = ${JSON.stringify(name)}`, `base_url = ${JSON.stringify(endpoint)}`,
      'wire_api = "responses"', 'requires_openai_auth = false',
      'http_headers = { "x-openai-actor-authorization" = "local-image-extension" }',
      `experimental_bearer_token = ${JSON.stringify(input.apiKey)}`,
      ...(input.apiFormat === 'openai_responses' ? [`model_catalog_url = ${JSON.stringify(root + '/v1/models')}`] : [])
    ].join('\n')
    entries.push(['configFormat', 'toml'], ['config', encodeCcSwitchBase64(config)])
  }
  const usageEnabled = input.usageEnabled !== false && root.startsWith('https://')
  entries.push(['usageEnabled', String(usageEnabled)])
  if (usageEnabled) {
    const interval = input.usageAutoInterval ?? 30
    if (!Number.isInteger(interval) || interval < 1 || interval > 1440) throw new Error('invalidInterval')
    entries.push(['usageBaseUrl', root], ['usageScript', encodeCcSwitchBase64(input.usageScript ?? CC_SWITCH_USAGE_SCRIPT)], ['usageAutoInterval', String(interval)])
  }
  return `ccswitch://v1/import?${new URLSearchParams(entries).toString()}`
}
