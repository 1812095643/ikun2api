import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { route, list } = vi.hoisted(() => ({
  route: { query: {} as Record<string, string> },
  list: vi.fn()
}))
vi.mock('vue-router', () => ({ useRoute: () => route }))
vi.mock('@/api/admin', () => ({ default: { subscriptions: { list } } }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('@/components/admin/subscription/UniversalSubscriptionsManager.vue', () => ({
  default: { template: '<div data-test="universal-subscriptions" />' }
}))
vi.mock('../orders/AdminPaymentPlansView.vue', () => ({
  default: { props: { embedded: Boolean }, template: '<div data-test="payment-plans" :data-embedded="embedded" />' }
}))

import SubscriptionsView from '../SubscriptionsView.vue'

describe('universal subscription management', () => {
  beforeEach(() => {
    route.query = {}
    list.mockReset()
    list.mockResolvedValue({ items: [{ id: 1, user_id: 42, group_id: 7, status: 'expired', expires_at: null }] })
  })

  it('starts with plans and switches to universal user subscriptions', async () => {
    const wrapper = mount(SubscriptionsView)
    expect(wrapper.get('[data-test="payment-plans"]').attributes('data-embedded')).toBe('true')
    await wrapper.findAll('[role="tab"]')[1]!.trigger('click')
    expect(wrapper.find('[data-test="universal-subscriptions"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="payment-plans"]').exists()).toBe(false)
    expect(list).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('opens user subscriptions directly for a user deep link', () => {
    route.query = { user_id: '42' }
    const wrapper = mount(SubscriptionsView)
    expect(wrapper.find('[data-test="universal-subscriptions"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="payment-plans"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('loads retired group subscriptions only when opening their read-only archive', async () => {
    const wrapper = mount(SubscriptionsView)
    expect(list).not.toHaveBeenCalled()
    await wrapper.findAll('[role="tab"]')[2]!.trigger('click')
    await flushPromises()
    expect(list).toHaveBeenCalledWith(1, 100)
    expect(wrapper.get('tbody').text()).toContain('expired')
    expect(wrapper.get('tbody').text()).toContain('42')
    expect(wrapper.get('section').find('button, input, select, form').exists()).toBe(false)
    expect(wrapper.find('[data-test="universal-subscriptions"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
