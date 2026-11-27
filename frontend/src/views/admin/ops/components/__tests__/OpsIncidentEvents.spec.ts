import { flushPromises, mount } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { reactive, ref } from 'vue'
import OpsIncidentEvents from '../OpsIncidentEvents.vue'

const api = vi.hoisted(() => ({ incidents: vi.fn(), health: vi.fn(), config: vi.fn(), replace: vi.fn() }))
vi.mock('@/api/admin/opsMonitor', () => ({ opsMonitorAPI: api }))
vi.mock('vue-router', () => ({ useRoute: () => reactive({ query: {} }), useRouter: () => ({ replace: api.replace }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => false }) }))
vi.mock('@vueuse/core', () => ({ useDocumentVisibility: () => ref('visible') }))
vi.mock('../OpsIncidentDetail.vue', () => ({ default: { template: '<div />' } }))
vi.mock('../OpsAlertEventsCard.vue', () => ({ default: { template: '<div />' } }))

beforeEach(() => {
  vi.clearAllMocks()
  api.incidents.mockResolvedValue({ items: [], has_more: false, before_time: 0, before_id: '' })
  api.health.mockResolvedValue({ complete: false, queue: { pending: 0 }, sources: [], health: { ready: false, loops: {} } })
  api.config.mockResolvedValue({ config: {}, rules: [] })
})

describe('运维事件列表', () => {
  it('沿用平台、分组和时间筛选，离页取消请求', async () => {
    const wrapper = mount(OpsIncidentEvents, { props: { filters: { platform: 'openai', group_id: 7, time_range: '1h' } }, global: { stubs: { DataTable: true, Select: true, Icon: true } } })
    await flushPromises()
    expect(api.incidents.mock.calls[0][0]).toMatchObject({ platform: 'openai', group_id: 7, time_range: '1h', status: 'active' })
    expect(wrapper.text()).toContain('admin.ops.enhanced.incomplete')
    const signal = api.incidents.mock.calls[0][1] as AbortSignal
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
  })

  it('切换筛选不保留上一页游标', async () => {
    const wrapper = mount(OpsIncidentEvents, { props: { filters: { group_id: 7 } }, global: { stubs: { DataTable: true, Select: true, Icon: true } } })
    await flushPromises(); await wrapper.setProps({ filters: { group_id: 8 } }); await flushPromises()
    expect(api.incidents.mock.calls.at(-1)?.[0]).toMatchObject({ group_id: 8 })
    expect(api.incidents.mock.calls.at(-1)?.[0].before_id).toBeUndefined()
    wrapper.unmount()
  })

  it('健康查询出错仍显示成功获取的事件，并单独提示健康信息不可用', async () => {
    api.incidents.mockResolvedValue({ items: [{ id: 'incident-1' }], has_more: false })
    api.health.mockRejectedValue(new Error('health unavailable'))
    const wrapper = mount(OpsIncidentEvents, { props: { filters: {} }, global: { stubs: { DataTable: true, Select: true, Icon: true, RouterLink: true } } })
    await flushPromises()
    expect(wrapper.getComponent({ name: 'DataTable' }).props('data')).toEqual([{ id: 'incident-1' }])
    expect(wrapper.text()).toContain('admin.ops.enhanced.healthUnavailable')
    wrapper.unmount()
  })

  it('顶部刷新信号不会打断后续页浏览，筛选变化仍回到第一页', async () => {
    api.incidents.mockResolvedValue({ items: [], has_more: true, before_time: 123, before_id: 'cursor' })
    const wrapper = mount(OpsIncidentEvents, { props: { filters: {}, refreshToken: 0 }, global: { stubs: { DataTable: true, Select: true, Icon: true, RouterLink: true } } })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.ops.enhanced.next')!.trigger('click')
    await flushPromises()
    expect(api.incidents.mock.calls.at(-1)?.[0].before_id).toBe('cursor')
    const count = api.incidents.mock.calls.length
    await wrapper.setProps({ refreshToken: 1 })
    await flushPromises()
    expect(api.incidents).toHaveBeenCalledTimes(count)
    expect(wrapper.text()).toContain('admin.ops.enhanced.returnLatest')
    await wrapper.setProps({ filters: { group_id: 2 } })
    await flushPromises()
    expect(api.incidents.mock.calls.at(-1)?.[0].before_id).toBeUndefined()
    wrapper.unmount()
  })
})
