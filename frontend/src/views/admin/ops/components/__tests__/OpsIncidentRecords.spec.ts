import { mount, flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import OpsIncidentRequests from '../OpsIncidentRequests.vue'
import OpsIncidentHistory from '../OpsIncidentHistory.vue'
import OpsDeliveryTable from '../OpsDeliveryTable.vue'
import type { MonitorDetail, MonitorIncident } from '@/api/admin/opsMonitor'

const api = vi.hoisted(() => ({ requests: vi.fn(), accounts: vi.fn(), history: vi.fn() }))
vi.mock('@/api/admin/opsMonitor', () => ({ opsMonitorAPI: api }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => true }) }))
beforeEach(() => vi.clearAllMocks())

describe('关联证据', () => {
  it('请求按游标翻页，错误和时间线入口指向所选请求，已删除账号有回退', async () => {
    const record = { request_id: 'r1', outcome: 'unknown', account_id: 1, account_name: '上游一', group_id: 1, group_name: '分组一', client_status: 0 }
    api.requests.mockResolvedValueOnce({ items: [record], has_more: true, after_id: 'r1' }).mockResolvedValue({ items: [{ ...record, request_id: 'r2' }], has_more: false, after_id: 'r2' })
    api.accounts.mockResolvedValue({ items: [{ id: 1, name: '上游一', deleted: true }], has_more: false })
    const wrapper = mount(OpsIncidentRequests, { props: { incident: { id: 'i1', account_count: 1 } as MonitorIncident }, global: { stubs: { RouterLink: { template: '<a><slot /></a>' } } } })
    await flushPromises()
    expect(wrapper.text()).toContain('admin.ops.enhanced.deleted')
    expect(wrapper.text()).toContain('admin.ops.enhanced.outcomes.unknown')
    await wrapper.findAll('button').find(button => button.text() === 'admin.ops.enhanced.requestErrors')!.trigger('click')
    expect(wrapper.emitted('errors')?.[0]).toEqual([record, 'request'])
    await wrapper.findAll('button').find(button => button.text() === 'admin.ops.enhanced.tabs.timeline')!.trigger('click')
    expect(wrapper.emitted('timeline')?.[0]).toEqual([record])
    await wrapper.findAll('button').find(button => button.text() === 'admin.ops.enhanced.next')!.trigger('click'); await flushPromises()
    expect(api.requests.mock.calls.at(-1)?.slice(0, 2)).toEqual(['i1', 'r1'])
    expect(wrapper.text()).toContain('r2')
    wrapper.unmount()
  })

  it('处理历史显示动作、操作者和固定快照时间，观测历史单独查询', async () => {
    const history = { transition_id: 1, status: 'firing', previous_status: 'firing', action: 'silence', actor: '7', actor_name: '值班管理员', recorded_at: '2026-11-01T00:00:00Z', silenced_until: '2026-11-02T00:00:00Z', control_reason: '维护' }
    const wrapper = mount(OpsIncidentHistory, { props: { detail: { incident: { id: 'i1', revision: 1 }, history: [history], history_has_more: false } as MonitorDetail } })
    expect(wrapper.text()).toContain('admin.ops.enhanced.historyActions.silence')
    expect(wrapper.text()).toContain('值班管理员')
    expect(wrapper.text()).toContain('维护')
    api.history.mockResolvedValue({ items: [], has_more: false, before_id: 0 })
    await wrapper.findAll('button').find(button => button.text() === 'admin.ops.enhanced.historyKinds.observations')!.trigger('click'); await flushPromises()
    expect(api.history.mock.calls[0].slice(0, 3)).toEqual(['i1', 0, 'observations'])
    wrapper.unmount()
  })

  it('投递记录可直接展示后台解析的对象名称及平台接收时间', () => {
    const wrapper = mount(OpsDeliveryTable, { props: { items: [{ id: 'd1', target: 't1', channel: 'c1', channel_name: '值班邮件', target_name: '值班管理员', state: 'accepted', attempts: 1, kind: 'firing', revision: 1, incident_id: 'i1', next_attempt: '2026-11-01T00:00:00Z', accepted_at: '2026-11-01T00:01:00Z' }] } })
    expect(wrapper.text()).toContain('值班邮件 · 值班管理员')
    expect(wrapper.text()).toContain('admin.ops.enhanced.deliveryColumns.accepted_at')
    expect(wrapper.text()).toContain(new Date('2026-11-01T00:01:00Z').toLocaleString())
    wrapper.unmount()
  })
})
