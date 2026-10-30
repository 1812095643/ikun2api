import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import LotteryEditor from '../LotteryEditor.vue'

const previewAPI = vi.hoisted(() => vi.fn())
vi.mock('@/api/lottery', async () => {
  const actual = await vi.importActual<typeof import('@/api/lottery')>('@/api/lottery')
  return { ...actual, lotteryAPI: { ...actual.lotteryAPI, preview: previewAPI } }
})
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const wrappers: VueWrapper[] = []
function setup() {
  const wrapper = mount(LotteryEditor, {
    props: { activity: null, busy: false, error: '' },
    global: { stubs: { Teleport: true, Icon: true } }
  })
  wrappers.push(wrapper)
  return wrapper
}
function button(wrapper: VueWrapper, text: string) {
  return wrapper.findAll('button').find(item => item.text() === text)!
}
async function lastStep(wrapper: VueWrapper) {
  await wrapper.get('#lottery-title').setValue('周末抽奖')
  await button(wrapper, '下一步').trigger('click')
  await button(wrapper, '下一步').trigger('click')
}

describe('抽奖三步弹窗', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    previewAPI.mockResolvedValue({ eligible_count: 20, prize_count: 6, prize_total: '15' })
  })
  afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

  it('空名称不能进入下一步，前后切换保留名称及参与条件', async () => {
    const wrapper = setup()
    await button(wrapper, '下一步').trigger('click')
    expect(wrapper.find('#lottery-title').exists()).toBe(true)
    await wrapper.get('#lottery-title').setValue('周末抽奖')
    await button(wrapper, '下一步').trigger('click')
    await button(wrapper, '当日消费').trigger('click')
    await wrapper.get('[aria-label="第 1 个条件值"]').setValue('8')
    await button(wrapper, '上一步').trigger('click')
    expect((wrapper.get('#lottery-title').element as HTMLInputElement).value).toBe('周末抽奖')
    await button(wrapper, '下一步').trigger('click')
    expect((wrapper.get('[aria-label="第 1 个条件值"]').element as HTMLInputElement).value).toBe('8')
    expect(wrapper.emitted('save')).toBeUndefined()
  })

  it('发布需先获取当前规则的后端预览并确认，修改奖金立即撤销确认', async () => {
    const wrapper = setup()
    await lastStep(wrapper)
    expect(button(wrapper, '发布抽奖').attributes('disabled')).toBeDefined()
    await button(wrapper, '查看符合人数').trigger('click')
    await flushPromises()
    await wrapper.get('.lottery-confirm-label input').setValue(true)
    expect(button(wrapper, '发布抽奖').attributes('disabled')).toBeUndefined()
    await wrapper.get('[aria-label="一等奖金额"]').setValue('25')
    expect(wrapper.find('.lottery-confirm-label').exists()).toBe(false)
    expect(button(wrapper, '发布抽奖').attributes('disabled')).toBeDefined()
    expect(wrapper.emitted('save')).toBeUndefined()
  })

  it('不接受请求过程中改过规则的旧预览', async () => {
    let resolve!: (value: { eligible_count: number; prize_count: number; prize_total: string }) => void
    previewAPI.mockImplementation(() => new Promise(done => { resolve = done }))
    const wrapper = setup()
    await lastStep(wrapper)
    await button(wrapper, '查看符合人数').trigger('click')
    await wrapper.get('[aria-label="一等奖金额"]').setValue('30')
    resolve({ eligible_count: 20, prize_count: 6, prize_total: '15' })
    await flushPromises()
    expect(wrapper.find('.lottery-confirm-label').exists()).toBe(false)
    expect(button(wrapper, '发布抽奖').attributes('disabled')).toBeDefined()
  })

  it('草稿保存保留全部字段并将北京时间转换为接口时间', async () => {
    const wrapper = setup()
    await lastStep(wrapper)
    await wrapper.get('input[type="datetime-local"]').setValue('2026-10-26T10:00')
    await wrapper.findAll('input[type="datetime-local"]')[1].setValue('2026-10-27T10:00')
    await button(wrapper, '保存草稿').trigger('click')
    expect(wrapper.emitted('save')?.[0]).toEqual([
      expect.objectContaining({ title: '周末抽奖', conditions: [], prizes: expect.any(Array), start_at: '2026-10-26T02:00:00.000Z', draw_at: '2026-10-27T02:00:00.000Z' }),
      false
    ])
  })

  it('保存期间禁止关闭和重复提交，后端提示在弹窗内显示', async () => {
    const wrapper = setup()
    await lastStep(wrapper)
    await wrapper.setProps({ busy: true, error: '请检查开奖人数' })
    await button(wrapper, '保存中…').trigger('click')
    await wrapper.trigger('keydown', { key: 'Escape' })
    expect(wrapper.emitted('save')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.get('[role="alert"]').text()).toBe('请检查开奖人数')
  })
})
