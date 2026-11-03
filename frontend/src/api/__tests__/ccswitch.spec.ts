import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchCcSwitchModels } from '../ccswitch'
afterEach(() => vi.unstubAllGlobals())
describe('CCS directory fetch', () => {
  it('reads only a model directory, without cookies or redirect forwarding', async () => {
    const fetch = vi.fn().mockResolvedValue({ ok: true, text: async () => JSON.stringify({ data: [{ id: 'current-model' }] }) })
    vi.stubGlobal('fetch', fetch)
    expect(await fetchCcSwitchModels('https://example.com/v1/', 'sk-test', 'openai')).toEqual(['current-model'])
    expect(fetch).toHaveBeenCalledWith('https://example.com/v1/models', expect.objectContaining({ method: 'GET', credentials: 'omit', redirect: 'error', cache: 'no-store', referrerPolicy: 'no-referrer', headers: { Accept: 'application/json', Authorization: 'Bearer sk-test' } }))
  })
  it('does not treat failed or invalid directory responses as valid model data', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false }))
    await expect(fetchCcSwitchModels('https://example.com', 'sk-test')).rejects.toThrow('modelsUnavailable')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, text: async () => '{"error":"no"}' }))
    await expect(fetchCcSwitchModels('https://example.com', 'sk-test')).rejects.toThrow('invalidModels')
  })
})
