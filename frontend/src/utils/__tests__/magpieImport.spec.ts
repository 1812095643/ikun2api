import { describe, expect, it } from 'vitest'
import { buildMagpieImportDeeplink, getMagpieFormats, resolveMagpieEndpoint } from '../magpieImport'
import type { CcSwitchGroup } from '../ccswitchImport'

const group = (platform: CcSwitchGroup['platform'] = 'anthropic', extra = {}): CcSwitchGroup => ({
  platform, claude_code_only: false, allow_claude_desktop: false,
  allow_messages_dispatch: false, default_mapped_model: '', ...extra
})
const base = { baseUrl: 'https://api.example.com/', providerName: '我的供应商 · 测试', apiKey: 'sk-synthetic+&?=#%', keyId: 18, group: group() }

describe('Magpie provider import protocol', () => {
  it.each([
    ['anthropic', 'anthropic', 'https://api.example.com'],
    ['openai_chat', 'chat', 'https://api.example.com/v1'],
    ['openai_responses', 'responses', 'https://api.example.com/v1'],
    ['gemini_native', 'gemini', 'https://api.example.com/v1beta']
  ] as const)('encodes %s using Magpie’s exact %s field', (format, field, endpoint) => {
    const url = new URL(buildMagpieImportDeeplink({ ...base, group: group('composite'), format, model: 'test-model-2026' }))
    expect(url.protocol).toBe('magpie:')
    expect(url.host).toBe('import')
    expect(url.searchParams.get(field)).toBe(endpoint)
    expect(url.searchParams.get('key')).toBe(base.apiKey)
    expect(url.searchParams.get('name')).toBe(base.providerName)
    expect(url.searchParams.get('models')).toBe('test-model-2026')
    expect(url.searchParams.get('id')).toMatch(/^ikun-[a-z0-9-]+$/)
    expect(url.searchParams.has('preset')).toBe(false)
    expect(url.searchParams.has('app')).toBe(false)
    expect([...url.searchParams.keys()].filter(k => ['anthropic', 'responses', 'chat', 'gemini'].includes(k))).toEqual([field])
  })
  it('keeps Chinese-only names importable without sharing ids across keys or formats', () => {
    const link = (keyId: number, format: 'anthropic' | 'openai_chat') => new URL(buildMagpieImportDeeplink({ ...base, keyId, format, providerName: '自营' }))
    expect(link(18, 'anthropic').searchParams.get('id')).not.toBe(link(19, 'anthropic').searchParams.get('id'))
    expect(link(18, 'anthropic').searchParams.get('id')).not.toBe(link(18, 'openai_chat').searchParams.get('id'))
    expect(link(18, 'anthropic').searchParams.has('models')).toBe(false)
  })
  it.each(['https://api.example.com', 'https://api.example.com/v1', 'https://api.example.com/v1/'])('normalizes SDK suffixes on %s', baseUrl => {
    expect(resolveMagpieEndpoint(baseUrl, 'openai_responses', group('openai'))).toBe('https://api.example.com/v1')
  })
  it('keeps Antigravity native routing without applying it to OpenAI routes', () => {
    expect(resolveMagpieEndpoint(base.baseUrl, 'anthropic', group('antigravity'))).toBe('https://api.example.com/antigravity')
    expect(resolveMagpieEndpoint(base.baseUrl, 'gemini_native', group('antigravity'))).toBe('https://api.example.com/antigravity/v1beta')
    expect(resolveMagpieEndpoint(base.baseUrl, 'openai_chat', group('antigravity'))).toBe('https://api.example.com/v1')
  })
  it('honors client and protocol restrictions without offering a bypass', () => {
    expect(getMagpieFormats(group('anthropic', { claude_code_only: true, allow_claude_desktop: true }))).toEqual([])
    expect(getMagpieFormats(group('typesafe'))).toEqual([])
    expect(getMagpieFormats(null)).toEqual([])
    expect(getMagpieFormats(group('openai'))).toEqual(['openai_responses', 'openai_chat'])
    expect(() => buildMagpieImportDeeplink({ ...base, group: group('openai'), format: 'anthropic' })).toThrow('unavailable')
  })
  it.each(['http://public.example.com', 'javascript:alert(1)', 'https://user:pass@example.com', 'https://example.com?key=bad', 'https://example.com/#bad'])('refuses unsafe or unsupported endpoints: %s', baseUrl => {
    expect(() => buildMagpieImportDeeplink({ ...base, baseUrl, format: 'anthropic' })).toThrow()
  })
  it.each([
    { providerName: '' }, { providerName: '中'.repeat(27) }, { providerName: 'bad\nname' },
    { apiKey: 'bad\nkey' }, { apiKey: 'a'.repeat(4097) }, { keyId: 0 }, { model: 'model,other' }
  ])('blocks invalid fields before opening the app: %s', extra => {
    expect(() => buildMagpieImportDeeplink({ ...base, format: 'anthropic', ...extra })).toThrow()
  })
})
