import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import OpsIncidentDetail from '../OpsIncidentDetail.vue'
import OpsIncidentStatus from '../OpsIncidentStatus.vue'
import type { MonitorDetail, MonitorIncident } from '@/api/admin/opsMonitor'

const api = vi.hoisted(() => ({ detail: vi.fn(), control: vi.fn(), listTraces: vi.fn(), getTrace: vi.fn() }))
vi.mock('@/api/admin/opsMonitor', () => ({ opsMonitorAPI: api }))
vi.mock('@/api/admin/connectionDiagnostics', () => ({ listConnectionTraces: api.listTraces, getConnectionTrace: api.getTrace }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => false }) }))
vi.mock('@vueuse/core', () => ({ useDocumentVisibility: () => ref('visible') }))
vi.mock('../OpsErrorDetailsModal.vue', () => ({ default: { name: 'OpsErrorDetailsModal', props: ['errorType', 'requestId', 'show'], template: '<div />' } }))
vi.mock('../OpsErrorDetailModal.vue', () => ({ default: { name: 'OpsErrorDetailModal', template: '<div />' } }))
vi.mock('../OpsIncidentRequests.vue', () => ({ default: { name: 'OpsIncidentRequests', template: '<div />' } }))
vi.mock('../OpsIncidentHistory.vue', () => ({ default: { template: '<div />' } }))
vi.mock('../OpsDeliveryTable.vue', () => ({ default: { template: '<div />' } }))

function data(revision = 1): MonitorDetail {
  return { incident: { id: 'i1', rule_id: 'F01', status: 'firing', revision, severity: 'P1', request_count: 1, account_count: 1,
    first_seen: '2026-11-27T01:00:00Z', last_seen: '2026-11-27T01:00:00Z', last_fault: '2026-11-27T01:00:00Z', group_id: 1,
    group_name: '测试分组', sample_account_name: '测试账号', sample: { request_id: 'r1', account_id: 2 }, explanation: { conclusion: 'failure', facts: [], inferences: [], missing_evidence: [], advice: [] }
  } as unknown as MonitorIncident, outcomes: { success: 0, failed: 1, cancelled: 0, unknown: 0 }, history: [], history_has_more: false, history_before_id: 0, deliveries: [] }
}
function mountDetail() {
  return mount(OpsIncidentDetail, { props: { id: 'i1', refreshToken: 0 }, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' }, Select: true, Icon: true, RouterLink: { template: '<a><slot /></a>' }, ConnectionTimeline: true } } })
}
function button(wrapper: ReturnType<typeof mountDetail>, key: string) { return wrapper.findAll('button').find(button => button.text() === 'admin.ops.enhanced.' + key)! }
beforeEach(() => { vi.clearAllMocks(); api.detail.mockResolvedValue(data()); api.control.mockResolvedValue({ saved: true }); api.listTraces.mockResolvedValue({ items: [], retention_days: 7 }) })

describe('事件详情排障与控制', () => {
  it('静默与确认不会盖住事件终态，单请求等待结束有独立语义', () => {
    const item = data().incident
    item.status = 'resolved'; item.acknowledged = true; item.silenced_until = '2099-01-01T00:00:00Z'
    const wrapper = mount(OpsIncidentStatus, { props: { item } })
    expect(wrapper.text()).toContain('admin.ops.enhanced.status.resolved')
    expect(wrapper.text()).toContain('admin.ops.enhanced.status.silenced')
    wrapper.unmount()
    const wait = mount(OpsIncidentStatus, { props: { item: { ...item, rule_id: 'L02', scope: 'ikun:r1' } } })
    expect(wait.text()).toContain('admin.ops.enhanced.status.waitEnded')
    wait.unmount()
  })

  it('409 后加载新版本，下一次操作不会继续提交旧 revision', async () => {
    api.detail.mockResolvedValueOnce(data(1)).mockResolvedValue(data(2))
    api.control.mockRejectedValueOnce({ status: 409 })
    const wrapper = mountDetail(); await flushPromises()
    await button(wrapper, 'ack').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('admin.ops.enhanced.conflictRefreshed')
    expect(api.detail).toHaveBeenCalledTimes(2)
    await button(wrapper, 'ack').trigger('click'); await flushPromises()
    expect(api.control.mock.calls.map(call => call[0].revision)).toEqual([1, 2])
    wrapper.unmount()
  })

  it('分别打开请求错误与上游尝试，不用样本账号过滤排除其他重试账号', async () => {
    const wrapper = mountDetail(); await flushPromises()
    await button(wrapper, 'sampleRequestErrors').trigger('click')
    let modal = wrapper.getComponent({ name: 'OpsErrorDetailsModal' })
    expect(modal.props()).toMatchObject({ errorType: 'request', requestId: 'r1', show: true })
    expect(modal.attributes('account-id')).toBeUndefined()
    await button(wrapper, 'sampleUpstreamAttempts').trigger('click')
    modal = wrapper.getComponent({ name: 'OpsErrorDetailsModal' })
    expect(modal.props('errorType')).toBe('upstream')
    wrapper.unmount()
  })

  it('冲突后的刷新若未成功，不会再次提交已过期的版本', async () => {
    api.detail.mockResolvedValueOnce(data(1)).mockRejectedValueOnce(new Error('offline')).mockResolvedValue(data(2))
    api.control.mockRejectedValueOnce({ status: 409 })
    const wrapper = mountDetail(); await flushPromises()
    await button(wrapper, 'ack').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('admin.ops.enhanced.conflictRefreshFailed')
    expect(button(wrapper, 'ack').attributes('disabled')).toBeDefined()
    await wrapper.get('button[aria-label="admin.ops.enhanced.refresh"]').trigger('click'); await flushPromises()
    expect(button(wrapper, 'ack').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('详情跟随父级刷新；连接加载失败明确提示且允许重试', async () => {
    const wrapper = mountDetail(); await flushPromises()
    await wrapper.setProps({ refreshToken: 1 }); await flushPromises()
    expect(api.detail).toHaveBeenCalledTimes(2)
    api.listTraces.mockRejectedValueOnce(new Error('network'))
    await wrapper.findAll('button').find(button => button.text() === 'admin.ops.enhanced.tabs.timeline')!.trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('admin.ops.enhanced.timelineStates.error')
    await button(wrapper, 'retry').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('admin.ops.enhanced.timelineStates.empty')
    wrapper.unmount()
  })
})
