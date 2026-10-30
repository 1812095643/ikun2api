import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ReloginView from './ReloginView.vue'
import ReloginAuthorizationForm from '@/components/workspace/relogin/ReloginAuthorizationForm.vue'

const api = vi.hoisted(() => ({
  accounts: vi.fn(), history: vi.fn(), settings: vi.fn(), options: vi.fn(), imported: vi.fn(), import: vi.fn(), start: vi.fn()
}))
vi.mock('@/api/relogin', async importOriginal => ({ ...await importOriginal<object>(), reloginAPI: api }))
vi.mock('@/composables/useStepUp', () => ({ useStepUp: () => ({}) }))

beforeEach(() => {
  vi.resetAllMocks()
  api.accounts.mockResolvedValue([])
  api.history.mockResolvedValue([])
  api.settings.mockResolvedValue({ settings: { auto_enabled: false }, quota: null })
  api.options.mockResolvedValue({ groups: [], proxies: [] })
  api.imported.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
  api.import.mockResolvedValue({ saved: 2 })
  api.start.mockResolvedValue({ id: 1, status: 'running' })
})

async function setup() {
  const wrapper = mount(ReloginView, {
    global: {
      stubs: {
        WorkspaceShell: { template: '<main><slot /></main>' },
        BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
        WorkspaceDialog: true, TotpStepUpDialog: true, Icon: true,
        ReloginAccounts: true, ReloginImported: true, ReloginHistory: true,
        RouterLink: true
      }
    }
  })
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === '账号授权')!.trigger('click')
  return wrapper
}

const items = [
  { email: 'first@example.test', password: 'private-first', auth_mode: 'password_2fa' },
  { email: 'second@example.test', password: 'private-second', auth_mode: 'password_2fa' }
]

describe('整行资料授权流程', () => {
  it('先保存资料，再使用保存资料的接口逐条授权并显示记录', async () => {
    const wrapper = await setup()
    wrapper.getComponent(ReloginAuthorizationForm).vm.$emit('import', items, true, true)
    await flushPromises()
    expect(api.import).toHaveBeenCalledWith(items)
    expect(api.start).toHaveBeenCalledTimes(2)
    expect(api.import.mock.invocationCallOrder[0]).toBeLessThan(api.start.mock.invocationCallOrder[0])
    expect(api.start).toHaveBeenNthCalledWith(1, {
      email: 'first@example.test', auth_mode: 'password_2fa', account_id: undefined, use_saved: true, apply: true
    })
    expect(wrapper.get('[role="status"]').text()).toContain('已提交 2 个授权任务')
    expect(wrapper.get('[aria-selected="true"]').text()).toBe('授权记录')
    wrapper.unmount()
  })
  it('仅保存时不发起授权，保存报错时也不会继续授权', async () => {
    const wrapper = await setup()
    wrapper.getComponent(ReloginAuthorizationForm).vm.$emit('import', items, false, true)
    await flushPromises()
    expect(api.start).not.toHaveBeenCalled()
    expect(wrapper.get('[aria-selected="true"]').text()).toContain('登录资料')
    await wrapper.findAll('button').find(button => button.text() === '账号授权')!.trigger('click')
    api.import.mockRejectedValueOnce(new Error('保存未完成'))
    wrapper.getComponent(ReloginAuthorizationForm).vm.$emit('import', items, true, true)
    await flushPromises()
    expect(api.start).not.toHaveBeenCalled()
    expect(wrapper.getComponent(ReloginAuthorizationForm).props('error')).toBe('保存未完成')
    wrapper.unmount()
  })
  it('部分提交报错时继续其他账号，分别显示数量和未确认记录', async () => {
    api.start.mockRejectedValueOnce(new Error('请求超时'))
    const wrapper = await setup()
    wrapper.getComponent(ReloginAuthorizationForm).vm.$emit('import', items, true, false)
    await flushPromises()
    expect(api.start).toHaveBeenCalledTimes(2)
    expect(api.start.mock.calls[1][0].apply).toBe(false)
    expect(wrapper.get('[role="status"]').text()).toContain('已提交 1 个授权任务')
    expect(wrapper.get('[role="alert"]').text()).toContain('first@example.test')
    expect(wrapper.get('[role="alert"]').text()).toContain('未确认提交')
    wrapper.unmount()
  })
})
