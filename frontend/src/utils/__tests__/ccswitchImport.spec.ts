import { describe, expect, it } from 'vitest'
import {
  CC_SWITCH_USAGE_SCRIPT, buildCcSwitchImportDeeplink, defaultCcSwitchClient,
  getCcSwitchClients, getCcSwitchFormats, parseCcSwitchModels,
  resolveCcSwitchEndpoint, encodeCcSwitchBase64
} from '@/utils/ccswitchImport'
import type { Group } from '@/types'

const base = { baseUrl: 'https://api.example.com/', providerName: '我的站 · 测试', apiKey: 'sk-test' }
const params = (url: string) => new URL(url).searchParams
const group = (platform: Group['platform'], extras: Partial<Group> = {}) => ({ platform, claude_code_only: false, allow_claude_desktop: false, allow_messages_dispatch: false, default_mapped_model: '', ...extras })
function link(clientType: Parameters<typeof buildCcSwitchImportDeeplink>[0]['clientType'], apiFormat: Parameters<typeof buildCcSwitchImportDeeplink>[0]['apiFormat'], model = 'test-model', overrides: Partial<Parameters<typeof buildCcSwitchImportDeeplink>[0]> = {}) {
  return params(buildCcSwitchImportDeeplink({ ...base, clientType, apiFormat, model, ...overrides }))
}

describe('CC Switch v4 provider import', () => {
  it.each([
    ['claude', 'anthropic', 'https://api.example.com', 'claude'],
    ['codex', 'openai_responses', 'https://api.example.com/v1', 'codex'],
    ['opencode', 'openai_chat', 'https://api.example.com/v1', 'opencode'],
    ['opencode', 'anthropic', 'https://api.example.com/v1', 'opencode'],
    ['pi', 'openai_chat', 'https://api.example.com/v1', 'pi'],
    ['gemini', 'gemini_native', 'https://api.example.com', 'gemini'],
    ['grokbuild', 'openai_responses', 'https://api.example.com/v1', 'grokbuild'],
    ['openclaw', 'openai_responses', 'https://api.example.com/v1', 'openclaw'],
    ['hermes', 'anthropic', 'https://api.example.com', 'hermes'],
    ['agy', 'gemini_native', 'https://api.example.com', 'agy']
  ] as const)('%s uses its own CCS app and SDK endpoint', (client, format, endpoint, app) => {
    const result = link(client, format)
    expect(result.get('app')).toBe(app)
    expect(result.get('apiFormat')).toBe(format)
    expect(result.get('endpoint')).toBe(endpoint)
    expect(result.get('model')).toBe('test-model')
    expect(result.get('resource')).toBe('provider')
  })
  it.each(['https://api.example.com', 'https://api.example.com/', 'https://api.example.com/v1', 'https://api.example.com/v1/'])('uses exactly one /v1 on %s', baseUrl => {
    expect(link('codex', 'openai_responses', 'test-model', { baseUrl }).get('endpoint')).toBe('https://api.example.com/v1')
  })
  it('uses the same-origin catalog and per-key config for Codex', () => {
    const result = link('codex', 'openai_responses')
    const config = atob(result.get('config') || '')
    expect(config).toContain('model_catalog_url = "https://api.example.com/v1/models"')
    expect(config).toContain('wire_api = "responses"')
    expect(config).toContain('web_search = "live"')
    expect(config).toContain('"x-openai-actor-authorization" = "local-image-extension"')
    expect(config).not.toContain('[features]')
    expect(config).toContain('experimental_bearer_token = "sk-test"')
    expect(result.get('configFormat')).toBe('toml')
  })
  it('leaves native Claude model selection to Claude and includes safe config', () => {
    const result = link('claude', 'anthropic', '', { platform: 'anthropic' })
    expect(result.has('model')).toBe(false)
    expect(result.has('sonnetModel')).toBe(false)
    expect(JSON.parse(atob(result.get('config') || ''))).toEqual({ env: {
      CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1',
      CLAUDE_CODE_ENABLE_TELEMETRY: '0'
    } })
  })
  it('maps non-native Claude tiers to the chosen model unless explicitly changed', () => {
    const result = link('claude', 'openai_responses', 'non-native', { platform: 'openai', opusModel: 'opus-alias' })
    expect(result.get('haikuModel')).toBe('non-native')
    expect(result.get('sonnetModel')).toBe('non-native')
    expect(result.get('opusModel')).toBe('opus-alias')
  })
  it('does not pretend Claude Desktop accepts a direct deep link', () => {
    const result = link('claude-desktop', 'anthropic', '', { platform: 'anthropic' })
    expect(result.get('app')).toBe('claude')
    expect(result.get('notes')).toContain('Claude Desktop')
    expect(JSON.parse(atob(result.get('config') || '')).env.CLAUDE_CODE_ENABLE_TELEMETRY).toBe('0')
  })
  it('respects an explicit choice to keep the existing telemetry configuration', () => {
    const result = link('claude', 'anthropic', '', { platform: 'anthropic', disableTelemetry: false })
    expect(result.has('config')).toBe(false)
    expect(result.has('configFormat')).toBe(false)
  })
  it('uses the Antigravity forced endpoint for both Claude and Gemini', () => {
    expect(resolveCcSwitchEndpoint('https://api.example.com/sub2api/', 'claude', 'anthropic', 'antigravity')).toBe('https://api.example.com/sub2api/antigravity')
    expect(resolveCcSwitchEndpoint('https://api.example.com/sub2api/', 'gemini', 'gemini_native', 'antigravity')).toBe('https://api.example.com/sub2api/antigravity')
  })
  it('includes a Unicode-safe usage script only for HTTPS', () => {
    const result = link('pi', 'openai_chat', 'test-model')
    expect(result.get('usageEnabled')).toBe('true')
    expect(atob(result.get('usageScript') || '')).toBe(CC_SWITCH_USAGE_SCRIPT)
    expect(result.get('usageBaseUrl')).toBe('https://api.example.com')
    expect(encodeCcSwitchBase64('中文')).toBe('5Lit5paH')
    const local = link('pi', 'openai_chat', 'test-model', { baseUrl: 'http://localhost:8080' })
    expect(local.get('usageEnabled')).toBe('false')
    expect(local.has('usageScript')).toBe(false)
  })
  it('rejects blank or expandable Pi model values', () => {
    expect(() => link('pi', 'openai_chat', '')).toThrow('invalidModel')
    expect(() => link('pi', 'openai_chat', '$SECRET')).toThrow('invalidModel')
    expect(() => link('pi', 'openai_chat', 'test-model', { apiKey: '!command' })).toThrow('dynamicValue')
  })
  it.each(['https://u:p@example.com', 'https://example.com/?key=secret', 'javascript:alert(1)', 'https://example.com/#x'])('rejects unsafe base URL %s', baseUrl => {
    expect(() => link('pi', 'openai_chat', 'test-model', { baseUrl })).toThrow('invalidEndpoint')
  })
  it('does not offer unauthorized clients for a restricted group', () => {
    const restricted = group('anthropic', { claude_code_only: true, allow_claude_desktop: true })
    expect(getCcSwitchClients(restricted).map(item => item.value)).toEqual(['claude', 'claude-desktop'])
    expect(getCcSwitchFormats('codex', restricted)).toEqual([])
    expect(defaultCcSwitchClient(group('openai'))).toBe('codex')
    expect(getCcSwitchClients(group('typesafe'))).toEqual([])
  })
  it('filters media-only models without inventing IDs', () => {
    expect(parseCcSwitchModels({ data: [{ id: 'claude-sonnet-4-6' }, { id: 'gpt-image-1' }, { id: 'glm-5' }, { id: '{file:secret}' }] })).toEqual(['claude-sonnet-4-6', 'glm-5'])
  })
})

describe('CCS balance extraction', () => {
  // Execute the standalone script as CCS does, including template substitution.
  const code = CC_SWITCH_USAGE_SCRIPT.replaceAll('{{baseUrl}}', 'https://api.example.com/v1/').replaceAll('{{apiKey}}', 'sk-test')
  // eslint-disable-next-line no-new-func
  const config = new Function(`return ${code}`)()
  it('uses the single same-origin balance endpoint', () => expect(config.request.url).toBe('https://api.example.com/v1/usage'))
  it('retains subscription details and never displays -1 as a negative balance', () => {
    expect(config.extractor({ isValid: true, planName: '不限量', remaining: -1, quota: { limit: 20, used: 3 } })).toEqual({ isValid: true, planName: '不限量', unit: 'USD', total: 20, used: 3 })
  })
})
