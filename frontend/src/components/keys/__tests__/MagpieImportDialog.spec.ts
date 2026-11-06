import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import MagpieImportDialog from '../MagpieImportDialog.vue'
import type { ApiKey } from '@/types'

const { fetchModels, copy, showError } = vi.hoisted(() => ({ fetchModels: vi.fn(), copy: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/ccswitch', () => ({ fetchCcSwitchModels: fetchModels }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: copy }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const makeKey = (id = 1, extra = {}) => ({ id, name: 'Synthetic', key: `sk-synthetic-${id}`, group: { name: 'Group', platform: 'openai', claude_code_only: false, ...extra } }) as ApiKey
const render = (apiKey = makeKey()) => mount(MagpieImportDialog, {
  props: { apiKey, baseUrl: 'https://example.com', siteName: '测试站' },
  global: { stubs: {
    BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }, Icon: true,
    Select: { props: ['id', 'modelValue', 'options'], emits: ['update:modelValue'], template: `<select :id="id" :value="modelValue" @change="$emit('update:modelValue', $event.target.value)"><option v-for="o in options" :key="o.value" :value="o.value">{{o.label}}</option></select>` }
  } }
})
beforeEach(() => { vi.clearAllMocks(); fetchModels.mockResolvedValue(['model-new', 'model-other']); vi.spyOn(window, 'open').mockReturnValue(null) })
afterEach(() => vi.restoreAllMocks())

describe('Magpie import dialog', () => {
  it('dispatches only on confirmation with the selected protocol and model, and masks the key', async () => {
    const w = render()
    await flushPromises()
    expect(window.open).not.toHaveBeenCalled()
    expect(w.text()).not.toContain('sk-synthetic-1')
    await w.get('#magpie-format').setValue('openai_chat')
    await w.get('#magpie-model').setValue('model-other')
    await w.get('[data-test="copy"]').trigger('click')
    const copied = copy.mock.calls[0][0]
    const link = new URL(copied)
    expect(link.searchParams.get('chat')).toBe('https://example.com/v1')
    expect(link.searchParams.get('key')).toBe('sk-synthetic-1')
    expect(link.searchParams.get('models')).toBe('model-other')
    await w.get('[data-test="import"]').trigger('click')
    expect(window.open).toHaveBeenCalledWith(copied, '_self')
    expect(w.find('[data-test="opened"]').exists()).toBe(true)
    w.unmount()
  })
  it('permits optional models after directory failure and preserves an explicit selection during refresh', async () => {
    fetchModels.mockRejectedValueOnce(new Error('offline'))
    const w = render()
    await flushPromises()
    expect(w.text()).toContain('keys.magpieImport.modelsFailed')
    expect(w.get('[data-test="import"]').attributes('disabled')).toBeUndefined()
    await w.get('[data-test="refresh-models"]').trigger('click'); await flushPromises()
    await w.get('#magpie-model').setValue('model-new')
    await w.get('[data-test="refresh-models"]').trigger('click'); await flushPromises()
    await w.get('[data-test="copy"]').trigger('click')
    expect(new URL(copy.mock.calls[0][0]).searchParams.get('models')).toBe('model-new')
    w.unmount()
  })
  it('aborts pending directory requests and ignores an old key’s late response', async () => {
    let resolveOld: (v: string[]) => void = () => {}
    fetchModels.mockImplementationOnce(() => new Promise<string[]>(resolve => { resolveOld = resolve }))
    const w = render()
    const signal = fetchModels.mock.calls[0][3] as AbortSignal
    await w.setProps({ apiKey: makeKey(2) }); await flushPromises()
    resolveOld(['old-key-model']); await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(w.findAll('#magpie-model option').map(o => o.attributes('value'))).not.toContain('old-key-model')
    await w.get('[data-test="copy"]').trigger('click')
    expect(new URL(copy.mock.calls[0][0]).searchParams.get('key')).toBe('sk-synthetic-2')
    w.unmount()
  })
  it('stops directory reads when returning to the method list', () => {
    fetchModels.mockReturnValue(new Promise(() => {}))
    const w = render()
    const signal = fetchModels.mock.calls[0][3] as AbortSignal
    w.unmount()
    expect(signal.aborted).toBe(true)
  })
  it('rejects a restricted group without reading its directory', async () => {
    const w = render(makeKey(1, { claude_code_only: true }))
    await flushPromises()
    expect(fetchModels).not.toHaveBeenCalled()
    expect(w.get('[data-test="import"]').attributes('disabled')).toBeDefined()
    w.unmount()
  })
})
