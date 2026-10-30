import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ReloginAuthorizationForm from './ReloginAuthorizationForm.vue'
import ReloginImportForm from './ReloginImportForm.vue'
import ReloginSettingsForm from './ReloginSettingsForm.vue'
import type { ReloginSettings } from '@/api/relogin'

describe('账号授权入口', () => {
  it('默认粘贴资料，切换手动填写不丢失已输入内容', async () => {
    const wrapper = mount(ReloginAuthorizationForm, { props: { account: null, busy: false, error: '' } })
    expect(wrapper.get('[aria-selected="true"]').text()).toBe('粘贴资料')
    await wrapper.get('textarea').setValue('user@example.test----pass----KEY')
    await wrapper.get('#relogin-auth-tab-manual').trigger('click')
    const manual = wrapper.get('#relogin-auth-panel-manual')
    expect(manual.isVisible()).toBe(true)
    await manual.get('input[type="email"]').setValue('manual@example.test')
    await wrapper.get('#relogin-auth-tab-paste').trigger('click')
    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('user@example.test----pass----KEY')
    await wrapper.get('#relogin-auth-tab-manual').trigger('click')
    expect(manual.get<HTMLInputElement>('input[type="email"]').element.value).toBe('manual@example.test')
    wrapper.unmount()
  })
  it('默认导入后授权，也能仅保存资料并保留写回选项', async () => {
    const wrapper = mount(ReloginImportForm, { props: { busy: false } })
    await wrapper.get('textarea').setValue('user@example.test----pass')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('import')?.[0]).toEqual([expect.any(Array), true, true])
    await wrapper.get('input[type="checkbox"]').setValue(false)
    await wrapper.get('button[type="button"]').trigger('click')
    expect(wrapper.emitted('import')?.[1]).toEqual([expect.any(Array), false, false])
    wrapper.unmount()
  })
  it('资料不匹配或提交中时不重复发送', async () => {
    const wrapper = mount(ReloginImportForm, { props: { busy: false, accountEmail: 'target@example.test' } })
    await wrapper.get('textarea').setValue('other@example.test----pass')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('[role="alert"]').text()).toContain('邮箱与当前账号不一致')
    expect(wrapper.emitted('import')).toBeUndefined()
    await wrapper.setProps({ busy: true })
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('import')).toBeUndefined()
    wrapper.unmount()
  })
})

describe('失效重登设置标签', () => {
  const settings: ReloginSettings = {
    auto_enabled: true, template: { proxy_id: null, concurrency: 100, priority: 1, rate_multiplier: 1, group_ids: [2] },
    quota_enabled: false, quota_interval: 300, quota_threshold: 20, recipients: ['owner@example.test']
  }
  const options = { groups: [{ id: 2, name: 'GPT' }], proxies: [{ id: 3, name: '默认代理' }] }
  it('每次只显示一类，切换保留改动并一次保存全部设置', async () => {
    const wrapper = mount(ReloginSettingsForm, { props: { settings, options, busy: false } })
    expect(wrapper.get('[aria-selected="true"]').text()).toBe('自动重登')
    expect(wrapper.get('#relogin-settings-panel-account').isVisible()).toBe(false)
    await wrapper.get('#relogin-settings-tab-account').trigger('click')
    await wrapper.get('#relogin-settings-panel-account input[type="number"]').setValue(80)
    await wrapper.get('#relogin-settings-tab-quota').trigger('click')
    await wrapper.get('textarea').setValue('owner@example.test\nsecond@example.test')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')?.[0]).toEqual([{
      ...settings, template: { ...settings.template, concurrency: 80 }, recipients: ['owner@example.test', 'second@example.test']
    }])
    expect(settings.template.concurrency).toBe(100)
    wrapper.unmount()
  })
  it('隐藏标签内有不合法输入时自动定位对应设置', async () => {
    const wrapper = mount(ReloginSettingsForm, { props: { settings, options, busy: false } })
    await wrapper.get('#relogin-settings-panel-quota input[type="number"]').trigger('invalid')
    expect(wrapper.get('[aria-selected="true"]').text()).toBe('额度通知')
    wrapper.unmount()
  })
})
