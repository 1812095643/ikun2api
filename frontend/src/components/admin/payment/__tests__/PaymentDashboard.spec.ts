import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import Dashboard from '@/views/admin/orders/AdminPaymentDashboardView.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import OrderStatsCards from '../OrderStatsCards.vue'
import DailyRevenueChart from '../DailyRevenueChart.vue'
import PaymentMethodChart from '../PaymentMethodChart.vue'
import zh from '@/i18n/locales/zh'
import type { DashboardStats } from '@/types/payment'
const { getDashboard, showError } = vi.hoisted(() => ({getDashboard:vi.fn(), showError:vi.fn()}))
vi.mock('@/api/admin/payment', () => ({adminPaymentAPI:{getDashboard}, default:{getDashboard}}))
vi.mock('@/stores/app', () => ({useAppStore:()=>({showError})}))
vi.mock('vue-chartjs', () => ({
  Line: {name:'PaymentLine',props:['data','options'],template:'<div />'},
  Bar: {name:'PaymentBar',props:['data','options'],template:'<div />'}
}))
enableAutoUnmount(afterEach)
const sample = (): DashboardStats => ({
  today_amount:{USD:21},total_amount:{USD:21,CNY:70}, today_count:2,total_count:3,avg_amount:{USD:10.5,CNY:70},
  pending_orders:1,order_count:4,paying_users:2,repeat_payers:1,paid_order_rate:75,
  refund_amount:{USD:27.5},refund_count:2,net_amount:{USD:-6.5,CNY:70},previous_amount:{USD:22},previous_count:1,
  order_statuses:[{status:'COMPLETED',count:3},{status:'PENDING',count:1}],order_types:[{type:'balance',amount:{USD:21},count:3}],
  hourly_series:[{hour:1,count:3}],daily_series:[{date:'2026-11-01',amount:{USD:21,CNY:70},count:3,refund:{USD:27.5},net:{USD:-6.5,CNY:70}}],
  payment_methods:[{type:'alipay',amount:{USD:21},count:3}],top_users:{}
})
const plugins=()=>[createI18n({legacy:false,locale:'zh',messages:{zh}})]
const setup=()=>mount(Dashboard,{global:{plugins:plugins(),stubs:{
  AppLayout:{template:'<main><slot/></main>'},DateRangePicker:true,OrderStatsCards:true,
  DailyRevenueChart:true,PaymentMethodChart:true,PaymentOrderStatusCard:true,PaymentHourlyChart:true,TopUsersLeaderboard:true,LoadingSpinner:true
}}})
beforeEach(()=>{getDashboard.mockReset().mockResolvedValue({data:sample()});showError.mockReset()})
describe('支付概览数据与筛选',()=>{
  it('日期与时区传到后端，统计卡与分析维度使用同一份结果',async()=>{
    const wrapper=setup()
    await flushPromises()
    expect(getDashboard).toHaveBeenCalledWith(expect.objectContaining({start_date:expect.any(String),end_date:expect.any(String),timezone:Intl.DateTimeFormat().resolvedOptions().timeZone}),expect.objectContaining({signal:expect.any(AbortSignal)}))
    expect(wrapper.getComponent(OrderStatsCards).props('stats')).toEqual(sample())
    expect(wrapper.findAllComponents(PaymentMethodChart)).toHaveLength(2)
  })
  it('快速切换日期取消旧请求，迟到响应不能覆盖新范围',async()=>{
    let resolveOld!:(value:unknown)=>void
    getDashboard.mockImplementationOnce(()=>new Promise(resolve=>{resolveOld=resolve}))
    const wrapper=setup()
    const oldSignal=getDashboard.mock.calls[0][1].signal
    wrapper.getComponent(DateRangePicker).vm.$emit('update:startDate','2026-11-01')
    await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    resolveOld({data:{...sample(),total_count:999}})
    await flushPromises()
    expect(wrapper.getComponent(OrderStatsCards).props('stats').total_count).toBe(3)
  })
  it('读取异常展示提示，不伪造全零统计',async()=>{
    getDashboard.mockRejectedValue(new Error('network unavailable'))
    const wrapper=setup()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).not.toBe('')
    expect(wrapper.findComponent(OrderStatsCards).exists()).toBe(false)
  })
})
describe('支付统计展示',()=>{
  it('币种独立展示，允许净收款为负，并显示上期对比',()=>{
    const wrapper=mount(OrderStatsCards,{props:{stats:sample()},global:{plugins:plugins()}})
    expect(wrapper.get('[data-testid="payment-stat-revenue"]').text()).toContain('21.00')
    expect(wrapper.get('[data-testid="payment-stat-revenue"]').text()).toContain('70.00')
    expect(wrapper.get('[data-testid="payment-stat-revenue"]').text()).toContain('-4.5%')
    expect(wrapper.get('[data-testid="payment-stat-net"]').text()).toContain('-US$6.50')
    expect(wrapper.findAll('[data-testid^="payment-stat-"]')).toHaveLength(8)
  })
  it('趋势可以切换退款、净收款和订单，金额不跨币种相加',async()=>{
    const wrapper=mount(DailyRevenueChart,{props:{data:sample().daily_series},global:{plugins:plugins(),stubs:{Line:true}}})
    const chart=()=>wrapper.findComponent({name:'PaymentLine'}).props('data')
    expect(chart().datasets.map((series:{label:string})=>series.label)).toEqual(['CNY','USD'])
    await wrapper.findAll('button')[2].trigger('click')
    expect(chart().datasets[0].data).toEqual([27.5])
    await wrapper.findAll('button')[3].trigger('click')
    expect(chart().datasets[0].data).toEqual([3])
  })
  it('支付方式显示同币种真实占比',()=>{
    const wrapper=mount(PaymentMethodChart,{props:{methods:[{type:'alipay',amount:{USD:10},count:1},{type:'wxpay',amount:{USD:30},count:1}]},global:{plugins:plugins()}})
    expect(wrapper.text()).toContain('25.0%')
    expect(wrapper.text()).toContain('75.0%')
  })
})
