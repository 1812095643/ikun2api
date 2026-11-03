import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CcSwitchImportDialog from '../CcSwitchImportDialog.vue'
import type { ApiKey } from '@/types'

const { fetchModels, copy, showError } = vi.hoisted(() => ({ fetchModels: vi.fn(), copy: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/ccswitch', () => ({ fetchCcSwitchModels: fetchModels }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: copy }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const makeKey = (id = 1, platform = 'anthropic', extra = {}) => ({ id, name: 'Test', key: `sk-test-${id}`, group: { name: 'Group', platform, claude_code_only: false, ...extra } }) as ApiKey
const mountDialog = (apiKey = makeKey()) => mount(CcSwitchImportDialog, {
  props: { apiKey, baseUrl: 'https://example.com', siteName: '测试站' },
  global: { stubs: {
    BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
    Icon: true,
    Select: { props: ['id', 'modelValue', 'options'], emits: ['update:modelValue'], template: `<select :id="id" :value="modelValue" @change="$emit('update:modelValue', $event.target.value)"><option value=""></option><option v-for="o in options" :key="o.value" :value="o.value">{{o.label}}</option></select>` }
  } }
})

beforeEach(() => {
  vi.clearAllMocks()
  fetchModels.mockResolvedValue(['model-new', 'model-other'])
  vi.spyOn(window, 'open').mockReturnValue(null)
})
afterEach(() => vi.restoreAllMocks())

describe('CCS import selection', () => {
  it('does not launch until the user selects and confirms a client', async () => {
    const w = mountDialog()
    await flushPromises()
    expect(window.open).not.toHaveBeenCalled()
    await w.get('#ccs-client').setValue('pi')
    expect(w.get('[data-test="import"]').attributes('disabled')).toBeDefined()
    await w.get('#ccs-model').setValue('model-new')
    await w.get('[data-test="import"]').trigger('click')
    const url = new URL(vi.mocked(window.open).mock.calls[0][0] as string)
    expect(url.searchParams.get('app')).toBe('pi')
    expect(url.searchParams.get('model')).toBe('model-new')
    expect(url.searchParams.get('endpoint')).toBe('https://example.com')
    expect(w.get('[data-test="opened"]').exists()).toBe(true)
    expect(showError).not.toHaveBeenCalled()
    w.unmount()
  })
  it('shows the Desktop two-step path without sending an unsupported app', async () => {
    const w = mountDialog()
    await flushPromises()
    await w.get('#ccs-client').setValue('claude-desktop')
    expect(w.get('[data-test="desktop-help"]').exists()).toBe(true)
    await w.get('[data-test="import"]').trigger('click')
    expect(new URL(vi.mocked(window.open).mock.calls[0][0] as string).searchParams.get('app')).toBe('claude')
    w.unmount()
  })
  it('honors restricted group options and masks the key', async () => {
    const w = mountDialog(makeKey(1, 'anthropic', { claude_code_only: true, allow_claude_desktop: false }))
    await flushPromises()
    expect(w.findAll('#ccs-client option').map(o => o.attributes('value'))).toEqual(['', 'claude'])
    expect(w.text()).not.toContain('sk-test-1')
    w.unmount()
  })
  it('aborts old key requests and ignores late responses after switching keys', async () => {
    let resolveOld: (v: string[]) => void = () => {}
    fetchModels.mockImplementationOnce(() => new Promise<string[]>(resolve => { resolveOld = resolve }))
    const w = mountDialog(makeKey(1, 'openai'))
    const signal = fetchModels.mock.calls[0][3] as AbortSignal
    await w.setProps({ apiKey: makeKey(2, 'openai') })
    await flushPromises()
    resolveOld(['old-key-only'])
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(w.findAll('#ccs-model option').map(o => o.attributes('value'))).not.toContain('old-key-only')
    expect(w.findAll('#ccs-model option').map(o => o.attributes('value'))).toContain('model-new')
    w.unmount()
  })
  it('does not discard a chosen model when refreshing the directory', async () => {
    const w = mountDialog(makeKey(1, 'openai'))
    await flushPromises()
    await w.get('#ccs-model').setValue('model-other')
    const refresh = w.findAll('button').find(b => b.text().includes('refreshModels'))!
    await refresh.trigger('click')
    await flushPromises()
    await w.get('[data-test="copy"]').trigger('click')
    const url = new URL(copy.mock.calls[0][0])
    expect(url.searchParams.get('model')).toBe('model-other')
    expect(url.searchParams.get('apiKey')).toBe('sk-test-1')
    w.unmount()
  })
})
