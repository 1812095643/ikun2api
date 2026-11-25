import { flushPromises, mount } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import OpsNotificationChannelDialog from '../OpsNotificationChannelDialog.vue'

const api = vi.hoisted(() => ({ saveChannel: vi.fn(), testChannel: vi.fn() }))
vi.mock('@/api/admin/opsMonitor', () => ({ opsMonitorAPI: api }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const global = { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }, Select: true, Toggle: true } }
beforeEach(() => { vi.clearAllMocks(); api.saveChannel.mockResolvedValue({ channels: [], targets: [] }) })

describe('通知渠道编辑', () => {
  it('新建默认停用并保留填写内容直至保存成功', async () => {
    api.saveChannel.mockRejectedValueOnce(new Error('保存遇到问题'))
    const wrapper = mount(OpsNotificationChannelDialog, { props: { show: true, targets: [] }, global })
    await wrapper.find('input[maxlength="100"]').setValue('运维通知')
    await wrapper.find('form').trigger('submit'); await flushPromises()
    expect(api.saveChannel.mock.calls[0][0].enabled).toBe(false)
    expect((wrapper.find('input[maxlength="100"]').element as HTMLInputElement).value).toBe('运维通知')
    expect(wrapper.emitted('close')).toBeUndefined()
    wrapper.unmount()
  })
  it('已有密钥不回填到表单，留空提交保持原值', async () => {
    const wrapper = mount(OpsNotificationChannelDialog, { props: { show: true, channel: { id: 'bot', kind: 'telegram', name: '通知机器人', enabled: true, settings: {}, revision: 2, secret_configured: ['bot_token'] }, targets: [{ id: 'target', channel_id: 'bot', name: '运维群', address: '-1001', enabled: true, settings: {} }] }, global })
    expect((wrapper.find('input[type="password"]').element as HTMLInputElement).value).toBe('')
    await wrapper.find('form').trigger('submit'); await flushPromises()
    expect(api.saveChannel.mock.calls[0][0].secrets).toEqual({})
    expect(api.saveChannel.mock.calls[0][0].clear_secrets).toEqual([])
    expect(wrapper.emitted('saved')).toHaveLength(1)
    wrapper.unmount()
  })
})
