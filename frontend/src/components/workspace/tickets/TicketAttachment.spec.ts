import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import TicketAttachment from './TicketAttachment.vue'

const imageRequest = vi.hoisted(() => vi.fn())
vi.mock('@/api/workspace', () => ({ ticketAPI: () => ({ image: imageRequest }) }))
const image = { id: 'image-1', name: 'screenshot.png', width: 1000, height: 700, size: 100, deleted: false }
const createUrl = vi.fn(() => 'blob:synthetic-ticket-image')
const revokeUrl = vi.fn()
beforeEach(() => {
  vi.resetAllMocks()
  createUrl.mockReturnValue('blob:synthetic-ticket-image')
  Object.defineProperty(URL, 'createObjectURL', { value: createUrl, configurable: true })
  Object.defineProperty(URL, 'revokeObjectURL', { value: revokeUrl, configurable: true })
  imageRequest.mockResolvedValue(new Blob(['synthetic'], { type: 'image/png' }))
})
function setup() { return mount(TicketAttachment, { props: { ticketId: 4, admin: true, image: { ...image } }, global: { stubs: { WorkspaceDialog: true } } }) }
describe('工单图片读取', () => {
  it('轮询返回相同图片对象副本时保留预览，不重新请求或撤销地址', async () => {
    const wrapper = setup()
    await flushPromises()
    for (let i = 0; i < 3; i++) { await wrapper.setProps({ image: { ...image } }); await flushPromises() }
    expect(imageRequest).toHaveBeenCalledTimes(1)
    expect(revokeUrl).not.toHaveBeenCalled()
    expect(wrapper.get('img').attributes('src')).toBe('blob:synthetic-ticket-image')
    wrapper.unmount()
    expect(revokeUrl).toHaveBeenCalledTimes(1)
  })
  it('读取失败后只在点击重试时重新请求', async () => {
    imageRequest.mockRejectedValueOnce(new Error('offline'))
    const wrapper = setup()
    await flushPromises()
    await wrapper.setProps({ image: { ...image } })
    expect(imageRequest).toHaveBeenCalledTimes(1)
    await wrapper.get('button.attachment-preview').trigger('click')
    await flushPromises()
    expect(imageRequest).toHaveBeenCalledTimes(2)
    expect(wrapper.find('img').exists()).toBe(true)
    wrapper.unmount()
  })
  it('切换工单或删除图片时取消旧读取并释放预览', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.setProps({ ticketId: 5 })
    await flushPromises()
    expect(imageRequest).toHaveBeenCalledTimes(2)
    await wrapper.setProps({ image: { ...image, deleted: true } })
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain('图片已由管理员删除')
    wrapper.unmount()
  })
})
