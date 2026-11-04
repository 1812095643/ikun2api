import { mount, flushPromises } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import PlanEditDialog from '../PlanEditDialog.vue'
const { createPlan, updatePlan } = vi.hoisted(() => ({ createPlan: vi.fn(), updatePlan: vi.fn() }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/payment', () => ({ adminPaymentAPI: { createPlan, updatePlan } }))
describe('通用套餐编辑器', () => {
 beforeEach(() => { createPlan.mockReset().mockResolvedValue({}); updatePlan.mockReset().mockResolvedValue({}) })
 it('creates an immutable-credit plan without binding a group', async () => {
  const wrapper = mount(PlanEditDialog, { props: { show: true, plan: null }, global: { plugins: [createPinia()], stubs: { BaseDialog: { template: '<div><slot/><slot name="footer"/></div>' } } } })
  await wrapper.get('#plan-name').setValue('月卡'); await wrapper.get('#plan-price').setValue('29'); await wrapper.get('#plan-total').setValue('100'); await wrapper.get('#daily_limit_usd').setValue('10'); await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(createPlan).toHaveBeenCalledTimes(1); const data = createPlan.mock.calls[0][0]; expect(data).toMatchObject({ plan_type: 'universal_credit', total_credit_usd: 100, daily_limit_usd: 10, validity_days: 30 }); expect(data).not.toHaveProperty('group_id'); expect(wrapper.emitted('saved')).toHaveLength(1)
 })
 it('does not submit an empty title or zero credit', async () => {
  const wrapper = mount(PlanEditDialog, { props: { show: true, plan: null }, global: { plugins: [createPinia()], stubs: { BaseDialog: { template: '<div><slot/></div>' } } } })
  await wrapper.get('form').trigger('submit'); expect(createPlan).not.toHaveBeenCalled(); expect(wrapper.get('[role="alert"]').text()).toContain('请填写')
 })
})
