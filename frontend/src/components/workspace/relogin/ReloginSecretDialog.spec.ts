import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ReloginSecretDialog from './ReloginSecretDialog.vue'
const secret = { email: 'owner@example.test', password: 'synthetic-password', mfa_secret: 'SYNTHETIC-MFA', inbox_url: 'https://mail.example.test/inbox', auth_mode: 'password_2fa' as const }
function setup() { return mount(ReloginSecretDialog, { props: { secret: { ...secret }, busy: false }, global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' } } } }) }
describe('失效重登资料查看', () => {
  it('直接查看并编辑关联资料，保存前不修改传入资料', async () => {
    const wrapper = setup()
    expect(wrapper.get<HTMLInputElement>('[aria-label="资料邮箱"]').element.value).toBe(secret.email)
    expect(wrapper.get<HTMLInputElement>('[aria-label="资料密码"]').element.value).toBe(secret.password)
    await wrapper.get('[aria-label="资料密码"]').setValue('updated-password')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')?.[0]).toEqual([{ ...secret, password: 'updated-password' }])
    expect(secret.password).toBe('synthetic-password')
    await wrapper.setProps({ secret: null })
    expect(wrapper.find('input').exists()).toBe(false)
    wrapper.unmount()
  })
  it('复制整行保留收件箱字段，提交中不重复保存', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    const wrapper = setup()
    await wrapper.findAll('button').find(button => button.text() === '复制整行')!.trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith([secret.email, secret.password, secret.mfa_secret, secret.inbox_url].join('----'))
    await wrapper.setProps({ busy: true })
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toBeUndefined()
    wrapper.unmount()
  })
})
