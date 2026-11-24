import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { describe, expect, it, vi } from 'vitest'
import ReloginAuthorizationForm from './ReloginAuthorizationForm.vue'
import ReloginImportForm from './ReloginImportForm.vue'
import ReloginSettingsForm from './ReloginSettingsForm.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import ModelWhitelistSelector from '@/components/account/ModelWhitelistSelector.vue'
import ReloginModelSettings from './ReloginModelSettings.vue'
import type { ReloginSettings } from '@/api/relogin'
vi.mock('vue-i18n', async original => ({
  ...(await original<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))

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
    auto_enabled: true, auto_error_threshold: 5, auto_error_window_minutes: 10, auto_cooldown_minutes: 360,
    template: { proxy_id: null, concurrency: 100, load_factor: 10000, priority: 1, rate_multiplier: 1, group_ids: [2], auto_pause_on_expired: true,
      extra: { codex_fingerprint_mode: 'full', openai_long_context_billing_enabled: true, openai_oauth_responses_websockets_v2_mode: 'ctx_pool', openai_oauth_responses_websockets_v2_enabled: true }, model_mapping: {} },
    quota_enabled: false, quota_interval: 300, quota_threshold: 20, recipients: ['owner@example.test']
  }
  const options = { groups: [{ id: 2, name: 'GPT' }], proxies: [] }
  it('认证异常阈值、窗口和冷却间隔可以独立保存', async () => {
    const wrapper = mount(ReloginSettingsForm, { props: { settings, options, busy: false }, global: { plugins: [createPinia()] } })
    await wrapper.get('#relogin-error-threshold').setValue(8)
    await wrapper.get('#relogin-error-window').setValue(15)
    await wrapper.get('#relogin-cooldown').setValue(60)
    await wrapper.get('form').trigger('submit')
    const saved = wrapper.emitted('save')?.[0][0] as ReloginSettings
    expect([saved.auto_error_threshold, saved.auto_error_window_minutes, saved.auto_cooldown_minutes]).toEqual([8, 15, 60])
    expect(settings.auto_error_threshold).toBe(5)
    wrapper.unmount()
  })
  it('代理、档位、开关与数字输入沿用账号编辑组件', async () => {
    const wrapper = mount(ReloginSettingsForm, { props: { settings, options, busy: false }, global: { plugins: [createPinia()] } })
    expect(wrapper.getComponent(ProxySelector).props('proxies')).toEqual([])
    expect(wrapper.findAllComponents(Select)).toHaveLength(2)
    expect(wrapper.findAllComponents(Toggle)).toHaveLength(4)
    expect(wrapper.findAll('#relogin-settings-panel-account input[type="number"].input')).toHaveLength(4)
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.get('fieldset').attributes('inert')).toBeUndefined()
    await wrapper.setProps({ busy: true })
    expect(wrapper.get('fieldset').attributes('inert')).toBeDefined()
    expect(wrapper.getComponent(ProxySelector).props('disabled')).toBe(true)
    wrapper.unmount()
  })
  it('复用账号分组选择组件，并保留倍率、账号数和平台筛选', async () => {
    const groupOptions = [{ id: 2, name: 'GPT', platform: 'openai' as const, rate_multiplier: 0.7, account_count: 5 }]
    const wrapper = mount(ReloginSettingsForm, { props: { settings, options: { ...options, groups: groupOptions }, busy: false }, global: { plugins: [createPinia()] } })
    const selector = wrapper.getComponent(GroupSelector)
    expect(selector.props('platform')).toBe('openai')
    expect(selector.props('groups')[0]).toEqual(groupOptions[0])
    selector.vm.$emit('update:modelValue', [])
    await wrapper.get('form').trigger('submit')
    expect((wrapper.emitted('save')?.[0][0] as ReloginSettings).template.group_ids).toEqual([])
    wrapper.unmount()
  })
  it('保存时移除选项中已经不存在的旧分组，保留其他默认值', async () => {
    const wrapper = mount(ReloginSettingsForm, { props: {
      settings: { ...settings, template: { ...settings.template, group_ids: [2, 82] } }, options, busy: false
    }, global: { plugins: [createPinia()] } })
    await wrapper.get('form').trigger('submit')
    const saved = wrapper.emitted('save')?.[0][0] as ReloginSettings
    expect(saved.template.group_ids).toEqual([2])
    expect(saved.template.extra).toEqual(settings.template.extra)
    expect(saved.template.load_factor).toBe(10000)
    wrapper.unmount()
  })
  it('每次只显示一类，切换保留改动并一次保存全部设置', async () => {
    const wrapper = mount(ReloginSettingsForm, { props: { settings, options, busy: false }, global: { plugins: [createPinia()] } })
    expect(wrapper.get('[aria-selected="true"]').text()).toBe('自动重登')
    expect(wrapper.get('#relogin-settings-panel-account').isVisible()).toBe(false)
    await wrapper.get('#relogin-settings-tab-account').trigger('click')
    await wrapper.get('#relogin-settings-panel-account input[type="number"]').setValue(80)
    await wrapper.get('#relogin-settings-tab-quota').trigger('click')
    await wrapper.get('#relogin-settings-panel-quota textarea').setValue('owner@example.test\nsecond@example.test')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')?.[0]).toEqual([{
      ...settings, template: { ...settings.template, concurrency: 80 }, recipients: ['owner@example.test', 'second@example.test']
    }])
    expect(settings.template.concurrency).toBe(100)
    wrapper.unmount()
  })
  it('保存收敛档位、模型映射及 WS 关闭选择，输入不回写原设置', async () => {
    const wrapper = mount(ReloginSettingsForm, { props: { settings, options, busy: false }, global: { plugins: [createPinia()] } })
    wrapper.findAllComponents(Select)[0].vm.$emit('update:modelValue', 'device')
    wrapper.findAllComponents(Select)[1].vm.$emit('update:modelValue', 'off')
    wrapper.getComponent(ReloginModelSettings).vm.$emit('update:modelMappings', [{ from: 'alias', to: 'target' }])
    await wrapper.get('form').trigger('submit')
    const saved = wrapper.emitted('save')?.[0][0] as ReloginSettings
    expect(saved.template.extra.codex_fingerprint_mode).toBe('device')
    expect(saved.template.extra.openai_oauth_responses_websockets_v2_enabled).toBe(false)
    expect(saved.template.model_mapping).toEqual({ alias: 'target' })
    expect(settings.template.extra.codex_fingerprint_mode).toBe('full')
    wrapper.unmount()
  })
  it('模型映射缺少目标时保留输入并阻止提交', async () => {
    const wrapper = mount(ReloginSettingsForm, { props: { settings, options, busy: false }, global: { plugins: [createPinia()] } })
    wrapper.getComponent(ReloginModelSettings).vm.$emit('update:modelMappings', [{ from: 'alias', to: '' }])
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toBeUndefined()
    expect(wrapper.get('[role="alert"]').text()).toContain('来源模型和上游模型')
    wrapper.unmount()
  })
  it('旧白名单、自定义模型和别名映射完整回填，切换与保存不丢失', async () => {
    const existing = { 'gpt-6': 'gpt-6', 'private-model': 'private-model', alias: 'gpt-6' }
    const wrapper = mount(ReloginSettingsForm, { props: {
      settings: { ...settings, template: { ...settings.template, model_mapping: existing } }, options, busy: false
    }, global: { plugins: [createPinia()] } })
    const models = wrapper.getComponent(ReloginModelSettings)
    expect(models.getComponent(ModelWhitelistSelector).props('modelValue')).toEqual(['gpt-6', 'private-model'])
    await models.findAll('[aria-pressed]')[1].trigger('click')
    expect(models.get<HTMLInputElement>('[aria-label="来源模型 1"]').element.value).toBe('alias')
    await models.get('[aria-label="上游模型 1"]').setValue('gpt-6.1-sol')
    await models.findAll('[aria-pressed]')[0].trigger('click')
    await wrapper.get('form').trigger('submit')
    expect((wrapper.emitted('save')?.[0][0] as ReloginSettings).template.model_mapping).toEqual({ ...existing, alias: 'gpt-6.1-sol' })
    expect(existing.alias).toBe('gpt-6')
    wrapper.unmount()
  })
  it('白名单可以搜索勾选、添加自定义模型和移除标签', async () => {
    const wrapper = mount(ReloginSettingsForm, { props: { settings, options, busy: false }, global: { plugins: [createPinia()] } })
    const selector = wrapper.getComponent(ModelWhitelistSelector)
    await selector.get('.cursor-pointer').trigger('click')
    await selector.get('input[placeholder="admin.accounts.searchModels"]').setValue('gpt-6.2-astra')
    expect(selector.findAll('[data-testid="model-option"]')).toHaveLength(1)
    await selector.get('[data-testid="select-model"]').trigger('click')
    await selector.get('input[placeholder="admin.accounts.enterCustomModelName"]').setValue('custom-model')
    await selector.get('input[placeholder="admin.accounts.enterCustomModelName"]').trigger('keydown.enter')
    await wrapper.get('form').trigger('submit')
    expect((wrapper.emitted('save')?.[0][0] as ReloginSettings).template.model_mapping).toEqual({ 'gpt-6.2-astra': 'gpt-6.2-astra', 'custom-model': 'custom-model' })
    await selector.get('.cursor-pointer .grid button').trigger('click')
    await wrapper.get('form').trigger('submit')
    expect((wrapper.emitted('save')?.[1][0] as ReloginSettings).template.model_mapping).toEqual({ 'custom-model': 'custom-model' })
    wrapper.unmount()
  })
  it('白名单清空后仍保留别名，完全清空时保存空配置', async () => {
    const wrapper = mount(ReloginSettingsForm, { props: {
      settings: { ...settings, template: { ...settings.template, model_mapping: { 'gpt-6': 'gpt-6', alias: 'gpt-6' } } }, options, busy: false
    }, global: { plugins: [createPinia()] } })
    const models = wrapper.getComponent(ReloginModelSettings)
    models.getComponent(ModelWhitelistSelector).vm.$emit('update:modelValue', [])
    await wrapper.get('form').trigger('submit')
    expect((wrapper.emitted('save')?.[0][0] as ReloginSettings).template.model_mapping).toEqual({ alias: 'gpt-6' })
    models.vm.$emit('update:modelMappings', [])
    await wrapper.get('#relogin-load-factor').setValue('')
    await wrapper.get('form').trigger('submit')
    const saved = wrapper.emitted('save')?.[1][0] as ReloginSettings
    expect(saved.template.model_mapping).toEqual({})
    expect(saved.template.load_factor).toBeNull()
    wrapper.unmount()
  })
  it.each([
    [{ from: 'gpt-*-x', to: 'target' }],
    [{ from: 'alias', to: 'gpt-*' }],
    [{ from: 'alias', to: 'first' }, { from: 'alias', to: 'second' }]
  ])('映射存在通配符错误或重复来源时阻止静默丢失配置 %#', async (...mappings) => {
    const wrapper = mount(ReloginSettingsForm, { props: { settings, options, busy: false }, global: { plugins: [createPinia()] } })
    wrapper.getComponent(ReloginModelSettings).vm.$emit('update:modelMappings', mappings)
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toBeUndefined()
    expect(wrapper.get('[role="alert"]').isVisible()).toBe(true)
    wrapper.unmount()
  })
  it('隐藏标签内有不合法输入时自动定位对应设置', async () => {
    const wrapper = mount(ReloginSettingsForm, { props: { settings, options, busy: false }, global: { plugins: [createPinia()] } })
    await wrapper.get('#relogin-settings-panel-quota input[type="number"]').trigger('invalid')
    expect(wrapper.get('[aria-selected="true"]').text()).toBe('额度通知')
    wrapper.unmount()
  })
})
