import { defineComponent, reactive } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useTicketNotification } from './useTicketNotification'

const api = vi.hoisted(() => ({ list: vi.fn(), getById: vi.fn(), notify: vi.fn() }))
const auth = reactive({ isAdmin: true, user: { id: 1 } })
vi.mock('@/api/admin', () => ({ adminAPI: { users: api } }))
vi.mock('@/api/workspace', () => ({ ticketAPI: () => api, workspaceError: (error: Error) => error.message }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
const user = { id: 8, email: 'owner@example.test', username: 'owner', status: 'active' }

beforeEach(() => {
  vi.resetAllMocks()
  auth.isAdmin = true
  auth.user = { id: 1 }
  api.list.mockResolvedValue({ items: [user] })
  api.getById.mockResolvedValue(user)
  api.notify.mockResolvedValue({ id: 42, already_sent: false })
})

function setup() {
  let state!: ReturnType<typeof useTicketNotification>
  const wrapper = mount(defineComponent({ setup() { state = useTicketNotification(); return () => null } }))
  return { state, wrapper }
}

describe('管理员定向工单通知', () => {
  it('复用账号接口按 ID 或邮箱搜索，必须明确选中收件人', async () => {
    const { state, wrapper } = setup()
    await state.search('#8')
    expect(api.getById).toHaveBeenCalledWith(8)
    expect(api.list).not.toHaveBeenCalled()
    expect(await state.submit({ content: '检查接口', images: [] })).toBeNull()
    expect(api.notify).not.toHaveBeenCalled()
    await state.search('owner@example.test')
    expect(api.list).toHaveBeenCalledWith(1, 20, { search: 'owner@example.test' }, { signal: expect.any(AbortSignal) })
    state.select(8)
    state.subject.value = '接口地址'
    expect(await state.submit({ content: '请检查地址', images: [] })).toBe(42)
    expect(api.notify).toHaveBeenCalledWith(expect.objectContaining({ user_id: 8, subject: '接口地址', content: '请检查地址', request_key: expect.any(String) }))
    wrapper.unmount()
  })
  it('超时重试沿用同一编号、收件人和内容，避免重复通知', async () => {
    const { state, wrapper } = setup()
    await state.search('owner')
    state.select(8)
    state.subject.value = '接口地址'
    api.notify.mockRejectedValueOnce(new Error('timeout'))
    expect(await state.submit({ content: '第一次内容', images: [] })).toBeNull()
    expect(state.uncertain.value).toBe(true)
    state.select(null)
    expect(state.recipient.value?.id).toBe(8)
    expect(await state.submit({ content: '改变内容', images: [] })).toBe(42)
    expect(api.notify.mock.calls[0][0]).toEqual(api.notify.mock.calls[1][0])
    expect(state.uncertain.value).toBe(false)
    wrapper.unmount()
  })
  it('停用账号与非管理员不能发送', async () => {
    api.list.mockResolvedValue({ items: [{ ...user, status: 'disabled' }] })
    const { state, wrapper } = setup()
    await state.search('owner')
    expect(state.options.value[0].disabled).toBe(true)
    state.select(8)
    state.subject.value = '通知'
    expect(await state.submit({ content: '检查接口', images: [] })).toBeNull()
    auth.isAdmin = false
    expect(await state.submit({ content: '检查接口', images: [] })).toBeNull()
    expect(api.notify).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('旧搜索响应不会覆盖新结果', async () => {
    let resolve!: (value: { items: typeof user[] }) => void
    api.list.mockReturnValueOnce(new Promise(done => { resolve = done }))
    const { state, wrapper } = setup()
    const first = state.search('old')
    await state.search('new')
    resolve({ items: [{ ...user, id: 9 }] })
    await first
    await flushPromises()
    expect(state.options.value[0].value).toBe(8)
    wrapper.unmount()
  })
})
