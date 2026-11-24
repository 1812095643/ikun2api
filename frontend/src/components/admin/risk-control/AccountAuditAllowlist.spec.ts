import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import AccountAuditAllowlist from './AccountAuditAllowlist.vue'

const api = vi.hoisted(() => ({ list: vi.fn(), getById: vi.fn() }))
vi.mock('@/api/admin/accounts', () => ({ accountsAPI: api }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const SelectStub = defineComponent({
  props: ['modelValue', 'options', 'loading'], emits: ['change', 'search', 'update:modelValue'],
  template: '<div data-test="select"><button data-test="add" @click="$emit(\'change\', \'22\')">add</button><button data-test="search" @click="$emit(\'search\', \'relay\')">search</button></div>'
})

describe('AccountAuditAllowlist', () => {
  beforeEach(() => {
    api.list.mockReset().mockResolvedValue({ items: [{ id: 22, name: 'Relay', platform: 'anthropic' }] })
    api.getById.mockReset().mockResolvedValue({ id: 11, name: 'Existing', platform: 'openai', credentials: { api_key: 'must-not-render' } })
  })

  it('adds and removes upstream accounts while keeping saved IDs outside the search page', async () => {
    const wrapper = mount(AccountAuditAllowlist, { props: { modelValue: [11] }, global: { stubs: { Select: SelectStub, Icon: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('Existing')
    expect(wrapper.text()).not.toContain('must-not-render')
    await wrapper.get('[data-test="add"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[11, 22]])
    await wrapper.setProps({ modelValue: [11, 22] })
    await wrapper.findAll('button').find(button => button.text().includes('Existing'))!.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[22]])
    wrapper.unmount()
  })

  it('keeps the saved whitelist and offers retry when search fails', async () => {
    api.list.mockRejectedValue(new Error('Network unavailable'))
    const wrapper = mount(AccountAuditAllowlist, { props: { modelValue: [11] }, global: { stubs: { Select: SelectStub, Icon: true } } })
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Existing')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })

  it('does not let an older account search overwrite a newer result', async () => {
    let resolveOld!: (value: unknown) => void
    api.list.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    const wrapper = mount(AccountAuditAllowlist, { props: { modelValue: [] }, global: { stubs: { Select: SelectStub, Icon: true } } })
    await wrapper.get('[data-test="search"]').trigger('click')
    await flushPromises()
    resolveOld({ items: [{ id: 33, name: 'Stale', platform: 'openai' }] })
    await flushPromises()
    expect(wrapper.findComponent(SelectStub).props('options')).toEqual([{ value: '22', label: 'Relay · anthropic · #22' }])
    wrapper.unmount()
  })
})
