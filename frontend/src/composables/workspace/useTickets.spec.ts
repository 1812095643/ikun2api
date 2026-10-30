import { defineComponent, reactive } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useTickets } from './useTickets'

const api = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), create: vi.fn(), delete: vi.fn(), cleanup: vi.fn() }))
const replace = vi.hoisted(() => vi.fn())
const route = reactive({ params: { id: '' } })
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ replace }) }))
vi.mock('@/api/workspace', () => ({ ticketAPI: () => api, workspaceError: (error: Error) => error.message }))

describe('工单限额与清理', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    route.params.id = ''
    api.list.mockResolvedValue({ items: [], total: 0, quota: { can_create: false, ticket_id: 3 } })
  })
  function setup() {
    let state!: ReturnType<typeof useTickets>
    const wrapper = mount(defineComponent({ setup() { state = useTickets(false); return () => null } }))
    return { state, wrapper }
  }
  it('新建被后端限制后刷新名额并保留提示', async () => {
    const { state, wrapper } = setup()
    await flushPromises()
    api.create.mockRejectedValue(new Error('今天已提交工单'))
    expect(await state.create({ content: '追加问题', images: [], subject: '标题' })).toBe(false)
    expect(state.result.value?.quota?.ticket_id).toBe(3)
    expect(state.error.value).toBe('今天已提交工单')
    expect(api.list).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
  it('删除请求没有成功时保留当前页面', async () => {
    const { state, wrapper } = setup()
    await flushPromises()
    api.delete.mockRejectedValue(new Error('请稍后重试'))
    expect(await state.deleteTicket(9)).toBe(false)
    expect(replace).not.toHaveBeenCalled()
    expect(state.busy.value).toBe(false)
    wrapper.unmount()
  })
  it('清理完成后返回删除数量并刷新列表', async () => {
    const { state, wrapper } = setup()
    await flushPromises()
    api.cleanup.mockResolvedValue({ deleted: 2 })
    expect(await state.cleanupEnded()).toBe(2)
    expect(api.list).toHaveBeenCalledTimes(2)
    expect(state.busy.value).toBe(false)
    wrapper.unmount()
  })
})
