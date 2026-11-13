import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import ReceivablePaymentDialog from '../ReceivablePaymentDialog.vue'
import DownstreamActionDialog from '../DownstreamActionDialog.vue'
import type { ReceivableAccount } from '@/api/admin/business'
const mocks = vi.hoisted(() => ({
  history: vi.fn(),
  payment: vi.fn(),
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
    users: { getById: mocks.getById }
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
})
describe('receivable collection', () => {
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
  it('edits the chosen group without introducing a grade or global multiplier', async () => {
    const w = mount(DownstreamActionDialog, {
      props: { mode: null, userIds: [8], initialGroup: 10 },
      global
    })
    await w.setProps({ mode: 'price' })
    await flushPromises()
    expect((w.get('#downstream-rate').element as HTMLInputElement).value).toBe(
      '0.5'
    )
    await w.get('#downstream-rate').setValue('0.3333')
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
    await w.get('#downstream-rate').setValue('0.12345')
    const confirm = w
      .findAll('button')
      .find((button) => button.text() === 'common.confirm')!
    expect(confirm.attributes('disabled')).toBeDefined()
    expect(mocks.apply).not.toHaveBeenCalled()
  })
})
