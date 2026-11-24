import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import MonitorConfigForm from '../MonitorConfigForm.vue'
import MonitorChannelsForm from '../MonitorChannelsForm.vue'
import type { MonitorChannels } from '@/api/admin/monitor'
import { monitorAPI, type MonitorSummary } from '@/api/admin/monitor'
vi.mock('@/api/admin/monitor', async importOriginal => ({ ...await importOriginal<typeof import('@/api/admin/monitor')>(), monitorAPI: { summary: vi.fn(), detail: vi.fn(), save: vi.fn(), control: vi.fn(), channels: vi.fn(), saveChannels: vi.fn(), feishuChats: vi.fn(), testChannel: vi.fn() } }))
const summary = (): MonitorSummary => ({ build: 'test', config: { version: 1, actor_id: 'admin:1', idempotency_key: 'old', shadow: true, site_id: 'test', rules: {}, profiles: { default: { queue_warn_seconds: 10, queue_fail_seconds: 30, first_warn_seconds: 30, first_fail_seconds: 60, stall_warn_seconds: 30, stall_fail_seconds: 60, task_sla_seconds: 0 } }, balance_low_usd: null, subscription_low_ratio: null, feishu_targets: [], email_targets: [], detail_base_url: '', payment_grace_seconds: 300, billing_grace_seconds: 120 }, incidents: [], deliveries: [], sources: [], capabilities: [], rules: [{ id: 'Q02', title: '额度低水位', advice: '核对额度', requires: ['configured_threshold'], recovery_samples: 1, recovery_seconds: 0 }], coverage_gaps: ['钱包独立流水缺失'], transports: { feishu: false, email: false }, checked_at: new Date().toISOString(), list_limit: 1000 })
beforeEach(() => vi.clearAllMocks())
describe('异常通知界面', () => {
  it('keeps unconfigured thresholds null and sends the displayed config revision', async () => {
    const state = summary(); vi.mocked(monitorAPI.save).mockImplementation(async c => ({ ...c, version: 2 }))
    const view = mount(MonitorConfigForm, { props: { config: state.config, rules: state.rules, transports: state.transports } })
    await view.get('form').trigger('submit'); await flushPromises()
    expect(monitorAPI.save).toHaveBeenCalledWith(expect.objectContaining({ version: 1, balance_low_usd: null, subscription_low_ratio: null, shadow: true }))
    expect(state.config.rules).toEqual({}); expect(view.text()).toContain('已保存配置 v2'); view.unmount()
  })
  it('leaves the Feishu connection blank and distinguishes it from a webhook URL', () => {
    const state = summary()
    const view = mount(MonitorConfigForm, { props: { config: state.config, rules: state.rules, transports: state.transports } })
    const target = view.get('textarea[placeholder*="chat_id"]')
    expect((target.element as HTMLTextAreaElement).value).toBe('')
    expect(view.text()).toContain('不是群 Webhook 链接')
    expect(view.text()).toContain('未接入任何通道时请保持影子模式')
    view.unmount()
  })
  it('keeps a version conflict visible without overwriting the draft', async () => {
    const state = summary(); vi.mocked(monitorAPI.save).mockRejectedValue({ response: { data: { message: '配置版本已变化，请刷新' } } })
    const view = mount(MonitorConfigForm, { props: { config: state.config, rules: state.rules, transports: state.transports } })
    await view.get('form').trigger('submit'); await flushPromises(); expect(view.get('[role="alert"]').text()).toContain('配置版本已变化'); expect(view.text()).not.toContain('已保存配置'); view.unmount()
  })
  it('saves email recipients and picks Feishu groups from the bot', async () => {
    const state = summary(); vi.mocked(monitorAPI.save).mockImplementation(async c => ({ ...c, version: 2 }))
    vi.mocked(monitorAPI.feishuChats).mockResolvedValue({ bot_name: '心链监控', chats: [{ chat_id: 'oc_ops1', name: '运维群' }, { chat_id: 'oc_ops2', name: '值班群' }] })
    const view = mount(MonitorConfigForm, { props: { config: state.config, rules: state.rules, transports: { feishu: true, email: true } } })
    await view.findAll('button').find(b => b.text() === '从飞书选择群')!.trigger('click'); await flushPromises()
    expect(view.text()).toContain('心链监控'); expect(view.text()).toContain('值班群')
    await view.findAll('input[type="checkbox"]').find(i => i.element.parentElement!.textContent!.includes('值班群'))!.trigger('change')
    await view.findAll('textarea')[1].setValue('ops@example.com\noncall@example.com')
    await view.get('form').trigger('submit'); await flushPromises()
    expect(monitorAPI.save).toHaveBeenCalledWith(expect.objectContaining({ feishu_targets: ['oc_ops2'], email_targets: ['ops@example.com', 'oncall@example.com'] }))
    view.unmount()
  })
  it('sends a test to every listed recipient and shows each result', async () => {
    const state = summary()
    vi.mocked(monitorAPI.testChannel).mockImplementation(async (channel) => { if (channel === 'email') throw { response: { data: { message: '邮件发送失败：535 认证失败' } } }; return { sent: true } })
    const view = mount(MonitorConfigForm, { props: { config: { ...state.config, feishu_targets: ['oc_ops1'], email_targets: ['ops@example.com'] }, rules: state.rules, transports: { feishu: true, email: true } } })
    await view.findAll('button').find(b => b.text().includes('发送测试通知'))!.trigger('click'); await flushPromises()
    expect(monitorAPI.testChannel).toHaveBeenCalledWith('feishu', 'oc_ops1'); expect(monitorAPI.testChannel).toHaveBeenCalledWith('email', 'ops@example.com')
    expect(view.text()).toContain('oc_ops1已发送'); expect(view.text()).toContain('535 认证失败'); expect(monitorAPI.save).not.toHaveBeenCalled()
    view.unmount()
  })
  it('never shows stored secrets and keeps them when left blank', async () => {
    const stored: MonitorChannels = { source: 'page', feishu: { domain: 'feishu', app_id: 'cli_abc12345678' }, feishu_secret_set: true, email: { host: 'smtp.qq.com', port: 465, security: 'ssl', username: 'ops@qq.com', from: 'ops@qq.com' }, email_password_set: true, updated_by: 'admin:1', updated_at: new Date().toISOString() }
    vi.mocked(monitorAPI.channels).mockResolvedValue(stored); vi.mocked(monitorAPI.saveChannels).mockResolvedValue(stored)
    const view = mount(MonitorChannelsForm); await flushPromises()
    const secrets = view.findAll('input[type="password"]')
    expect(secrets.every(i => (i.element as HTMLInputElement).value === '')).toBe(true)
    expect(secrets[0].attributes('placeholder')).toContain('留空保持不变')
    await view.findAll('button').find(b => b.text() === 'QQ 邮箱')!.trigger('click')
    await view.get('form[aria-label="邮箱 SMTP"]').trigger('submit'); await flushPromises()
    expect(monitorAPI.saveChannels).toHaveBeenCalledWith({ feishu: { domain: 'feishu', app_id: 'cli_abc12345678', app_secret: '' }, email: { host: 'smtp.qq.com', port: 465, security: 'ssl', username: 'ops@qq.com', password: '', from: 'ops@qq.com' } })
    expect(view.emitted('saved')).toBeTruthy()
    view.unmount()
  })
  it('shows the platform reason when Feishu verification fails', async () => {
    const empty: MonitorChannels = { source: 'none', feishu: { domain: 'feishu', app_id: '' }, feishu_secret_set: false, email: { host: '', port: 0, security: 'ssl', username: '', from: '' }, email_password_set: false, updated_at: '0001-01-01T00:00:00Z' }
    vi.mocked(monitorAPI.channels).mockResolvedValue(empty); vi.mocked(monitorAPI.saveChannels).mockRejectedValue({ response: { data: { message: '飞书验证失败：App ID 或 App Secret 不正确，或应用尚未发布' } } })
    const view = mount(MonitorChannelsForm); await flushPromises()
    await view.get('input[placeholder="cli_…"]').setValue('cli_abc12345678')
    await view.get('form[aria-label="飞书机器人"]').trigger('submit'); await flushPromises()
    expect(view.get('[role="alert"]').text()).toContain('应用尚未发布'); expect(view.emitted('saved')).toBeFalsy()
    view.unmount()
  })
})
