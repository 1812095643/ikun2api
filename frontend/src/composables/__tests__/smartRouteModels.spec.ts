import { effectScope } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { ApiKey } from '@/types'
import { useCcSwitchImport } from '../useCcSwitchImport'
import { useMagpieImport } from '../useMagpieImport'
const { fetchModels } = vi.hoisted(() => ({ fetchModels: vi.fn().mockResolvedValue(['claude-test', 'gpt-test', 'gemini-test']) }))
vi.mock('@/api/ccswitch', () => ({ fetchCcSwitchModels: fetchModels }))
const group = { id: 1, name: 'Antigravity', platform: 'antigravity', claude_code_only: false } as ApiKey['group']

describe('smart-key model discovery', () => {
  it.each([['CC-Switch', useCcSwitchImport], ['Magpie', useMagpieImport]] as const)('%s reads the merged key catalog without changing import formats', async (_, useImport) => {
    fetchModels.mockClear()
    const scope = effectScope()
    const key = { id: 5, name: 'Smart', key: 'local-synthetic-key', group, group_id: 1, group_ids: [1, 2] } as ApiKey
    const state = scope.run(() => useImport({ apiKey: () => key, baseUrl: () => 'https://local.test.invalid', siteName: () => 'IKUN2api' }))!
    await flushPromises()
    expect(fetchModels).toHaveBeenLastCalledWith('https://local.test.invalid', key.key, undefined, expect.any(AbortSignal))
    expect(state.models.value).toEqual(['claude-test', 'gpt-test', 'gemini-test'])
    expect(state.group.value?.platform).toBe('antigravity')
    scope.stop()
  })
})
