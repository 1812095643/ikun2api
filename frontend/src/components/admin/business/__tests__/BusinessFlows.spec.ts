import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import ReceivablePaymentDialog from '../ReceivablePaymentDialog.vue'
import DownstreamActionDialog from '../DownstreamActionDialog.vue'
import ReceivableCreditDialog from '../ReceivableCreditDialog.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import type { ReceivableAccount } from '@/api/admin/business'
const mocks = vi.hoisted(() => ({
  history: vi.fn(),
  payment: vi.fn(),
  credit: vi.fn(),
  list: vi.fn(),
  apply: vi.fn(),
  getAllIncludingInactive: vi.fn(),
  getById: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))
vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('@/api/admin/business', async (original) => ({
  ...(await original<typeof import('@/api/admin/business')>()),
  businessAPI: mocks
}))
vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: { getAllIncludingInactive: mocks.getAllIncludingInactive },
    users: { getById: mocks.getById, list: mocks.list }
  }
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const user: ReceivableAccount = {
  user_id: 8,
  email: 'reseller@example.invalid',
  username: '',
  deleted: false,
  is_downstream: true,
  balance: '7',
  credited: '10',
  received: '0',
  outstanding: '10'
}
const global = {
  stubs: {
    BaseDialog: {
      props: ['show'],
      template: '<div v-if="show"><slot /><slot name="footer" /></div>'
    },
    BusinessError: true
  }
}
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.clearAllMocks()
  mocks.history.mockResolvedValue({ account: user, items: [], total: 0 })
  mocks.getAllIncludingInactive.mockResolvedValue([
    { id: 10, name: 'Claude', platform: 'anthropic', rate_multiplier: 1 }
  ])
  mocks.getById.mockResolvedValue({ id: 8, group_rates: { 10: 0.5, 11: 0.6 } })
  mocks.apply.mockResolvedValue({ affected: 1 })
  mocks.list.mockResolvedValue({ items: [{ id: 8, email: user.email }], total: 1 })
})
describe('下游购买限制', () => {
  it('默认关闭，批量开启与关闭都只修改选中下游的购买限制', async () => {
    const wrapper = mount(DownstreamActionDialog, { props: { mode: 'purchase', userIds: [8, 9] }, global })
    expect(wrapper.getComponent(Toggle).props('modelValue')).toBe(false)
    wrapper.getComponent(Toggle).vm.$emit('update:modelValue', true)
    await wrapper.vm.$nextTick()
    const confirm = wrapper.findAll('button').find(button => button.text() === 'common.confirm')!
    await confirm.trigger('click')
    await flushPromises()
    expect(mocks.apply).toHaveBeenCalledWith({ user_ids: [8, 9], downstream_recharge_only: true, operation_key: expect.any(String) })
    wrapper.getComponent(Toggle).vm.$emit('update:modelValue', false)
    await wrapper.vm.$nextTick()
    await confirm.trigger('click')
    await flushPromises()
    expect(mocks.apply.mock.lastCall?.[0]).toMatchObject({downstream_recharge_only:false})
    expect(mocks.getAllIncludingInactive).not.toHaveBeenCalled()
  })
})

describe('receivable collection', () => {
  it('补录仅调用记账接口，超时重试使用同一操作编号', async () => {
    mocks.credit.mockRejectedValue({ message: 'timeout' })
    const w = mount(ReceivableCreditDialog, { global })
    await flushPromises()
    w.getComponent(Select).vm.$emit('update:modelValue', 8)
    await w.get('#receivable-credit-amount').setValue('7.12345678')
    await w.get('form').trigger('submit')
    await flushPromises()
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.credit).toHaveBeenCalledTimes(2)
    expect(mocks.credit.mock.calls[0][1].amount).toBe('7.12345678')
    expect(mocks.credit.mock.calls[1][1].operation_key).toBe(mocks.credit.mock.calls[0][1].operation_key)
    expect(mocks.payment).not.toHaveBeenCalled()
    expect(w.emitted('saved')).toBeUndefined()
  })
  it('preserves exact decimal text and the operation id when retrying a timeout', async () => {
    mocks.payment.mockRejectedValue({ message: 'timeout' })
    const w = mount(ReceivablePaymentDialog, { props: { user }, global })
    await flushPromises()
    await w.get('#receipt-amount').setValue('3.12345678')
    await w.get('form').trigger('submit')
    await flushPromises()
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.payment.mock.calls[0][1].amount).toBe('3.12345678')
    expect(mocks.payment.mock.calls[1][1].operation_key).toBe(
      mocks.payment.mock.calls[0][1].operation_key
    )
    expect(w.emitted('saved')).toBeUndefined()
  })
  it('blocks an amount above the freshly loaded debt', async () => {
    mocks.history.mockResolvedValue({
      account: { ...user, outstanding: '2' },
      items: [],
      total: 0
    })
    const w = mount(ReceivablePaymentDialog, { props: { user }, global })
    await flushPromises()
    await w.get('#receipt-amount').setValue('3')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.payment).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith('business.excessPayment')
  })
})
describe('existing per-group multiplier shortcut', () => {
  it('批量调价显示公开价格与混合专属价格，仅保存修改过的分组', async () => {
    mocks.getAllIncludingInactive.mockResolvedValue([
      { id: 10, name: 'Claude', platform: 'anthropic', rate_multiplier: 1 },
      { id: 11, name: 'GPT', platform: 'openai', rate_multiplier: 0.8 }
    ])
    const users = [
      { id: 8, group_rates: [{ group_id: 10, rate: 0.5 }] },
      { id: 9, group_rates: [{ group_id: 10, rate: 0.6 }] }
    ] as import('@/api/admin/business').DownstreamUser[]
    const w = mount(DownstreamActionDialog, { props: { mode: 'price', userIds: [8, 9], users }, global })
    await flushPromises()
    expect(w.get('#downstream-rate-10').attributes('placeholder')).toBe('business.mixedRate')
    expect(w.text()).toContain('business.publicRate')
    await w.get('#downstream-rate-11').setValue('0.4')
    await w.findAll('button').find(button => button.text() === 'common.confirm')!.trigger('click')
    await flushPromises()
    expect(mocks.apply).toHaveBeenCalledWith({ user_ids: [8, 9], group_rates: { 11: 0.4 }, operation_key: expect.any(String) })
  })
  it('edits the chosen group without introducing a grade or global multiplier', async () => {
    const w = mount(DownstreamActionDialog, {
      props: { mode: null, userIds: [8], initialGroup: 10 },
      global
    })
    await w.setProps({ mode: 'price' })
    await flushPromises()
    expect((w.get('#downstream-rate-10').element as HTMLInputElement).value).toBe(
      '0.5'
    )
    await w.get('#downstream-rate-10').setValue('0.3333')
    const confirm = w
      .findAll('button')
      .find((button) => button.text() === 'common.confirm')!
    await confirm.trigger('click')
    await flushPromises()
    expect(mocks.apply).toHaveBeenCalledWith({
      user_ids: [8],
      group_rates: { 10: 0.3333 },
      operation_key: expect.any(String)
    })
  })
  it('prevents a multiplier from being silently rounded by database precision', async () => {
    const w = mount(DownstreamActionDialog, {
      props: { mode: null, userIds: [8], initialGroup: 10 },
      global
    })
    await w.setProps({ mode: 'price' })
    await flushPromises()
    await w.get('#downstream-rate-10').setValue('0.12345')
    const confirm = w
      .findAll('button')
      .find((button) => button.text() === 'common.confirm')!
    expect(confirm.attributes('disabled')).toBeDefined()
    expect(mocks.apply).not.toHaveBeenCalled()
  })
})
