import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import TicketConversation from './TicketConversation.vue'
import type { Ticket } from '@/api/workspace'

const message = { id: 1, content: 'message', kind: 'message', author_role: 'user', author_name: '用户', author_id: 1, event_type: '', created_at: '2026-11-11T10:00:00Z', images: [] }
const ticket = { id: 1, subject: '工单测试', status: 'open', origin: 'user', messages: [message] } as unknown as Ticket
function setup() { return mount(TicketConversation, { props: { ticket: structuredClone(ticket), admin: true, busy: false, submit: vi.fn() }, global: { stubs: { TicketComposer: true, TicketAttachment: true, Icon: true } } }) }
describe('工单滚动位置', () => {
  it('相同内容轮询时不重设滚动位置', async () => {
    const wrapper = setup()
    await flushPromises()
    const el = wrapper.get('.conversation-messages').element as HTMLElement
    Object.defineProperties(el, { scrollHeight: { value: 600, configurable: true }, clientHeight: { value: 500, configurable: true } })
    el.scrollTop = 90
    await wrapper.setProps({ ticket: structuredClone(ticket) })
    await flushPromises()
    expect(el.scrollTop).toBe(90)
    wrapper.unmount()
  })
  it('正在阅读历史消息时有新消息也保留位置，接近底部才跟随', async () => {
    const wrapper = setup()
    await flushPromises()
    const el = wrapper.get('.conversation-messages').element as HTMLElement
    Object.defineProperties(el, { scrollHeight: { value: 1000, configurable: true }, clientHeight: { value: 500, configurable: true } })
    el.scrollTop = 20
    const next = { ...ticket, messages: [message, { ...message, id: 2 }] }
    await wrapper.setProps({ ticket: next })
    await flushPromises()
    expect(el.scrollTop).toBe(20)
    el.scrollTop = 480
    await wrapper.setProps({ ticket: { ...next, messages: [...next.messages, { ...message, id: 3 }] } })
    await flushPromises()
    expect(el.scrollTop).toBe(1000)
    wrapper.unmount()
  })
})
