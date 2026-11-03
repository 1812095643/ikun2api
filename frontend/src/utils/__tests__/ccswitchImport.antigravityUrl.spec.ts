import { describe, expect, it } from 'vitest'
import { buildCcSwitchImportDeeplink } from '../ccswitchImport'

describe('Antigravity CC Switch endpoint', () => {
  it.each([
    ['https://api.example.com', 'https://api.example.com/antigravity'],
    ['https://api.example.com/', 'https://api.example.com/antigravity'],
    ['https://api.example.com///', 'https://api.example.com/antigravity'],
    ['https://api.example.com/sub2api/', 'https://api.example.com/sub2api/antigravity']
  ])('joins the platform path onto %s for both native clients', (baseUrl, endpoint) => {
    for (const [clientType, apiFormat] of [['claude', 'anthropic'], ['gemini', 'gemini_native']] as const) {
      const url = new URL(buildCcSwitchImportDeeplink({
        baseUrl, clientType, apiFormat, platform: 'antigravity', providerName: 'IKUN2api',
        apiKey: 'sk-test', model: 'test-model'
      }))
      expect(url.searchParams.get('endpoint')).toBe(endpoint)
      expect(url.searchParams.get('app')).toBe(clientType)
    }
  })
})
