import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import DownstreamManager from '../DownstreamManager.vue'
import DownstreamTable from '../DownstreamTable.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'

const downstreams = vi.hoisted(() => vi.fn().mockResolvedValue({items: [], total: 30, page: 1, page_size: 20, summary:{balance:'0'}}))
vi.mock('@/api/admin/business', () => ({businessAPI: {downstreams}}))
vi.mock('@/stores/app', () => ({useAppStore: () => ({showError:vi.fn()})}))
vi.mock('vue-i18n', async original => ({...await original<typeof import('vue-i18n')>(),useI18n: () => ({t:(key:string)=>key})}))
enableAutoUnmount(afterEach)
describe('下游全列表排序', () => {
  it('把排序传到服务端并重置页码，翻页继续使用同一排序', async () => {
    downstreams.mockClear()
    const wrapper = mount(DownstreamManager, {global:{stubs:{
      TablePageLayout:{template:'<main><slot name="filters"/><slot name="table"/><slot name="pagination"/></main>'},
      DataTable:true, Pagination:true, DownstreamActionDialog:true, DownstreamUsageDialog:true,
      ReceivableHistoryDialog:true, UserBalanceModal:true, Select:true, Icon:true, RouterLink:true
    }}})
    await flushPromises()
    const table=wrapper.getComponent(DownstreamTable)
    expect(table.getComponent(DataTable).props('serverSideSort')).toBe(true)
    expect(table.getComponent(DataTable).props('columns').filter((col:{sortable:boolean})=>col.sortable).map((col:{key:string})=>col.key)).toEqual(['user','downstream_recharge_only','balance','outstanding','requests','tokens','cost','last_used_at'])
    wrapper.getComponent(Pagination).vm.$emit('update:page', 2)
    await flushPromises()
    table.vm.$emit('sort','cost','asc')
    await flushPromises()
    expect(downstreams.mock.lastCall?.[0]).toMatchObject({sort_by:'cost',sort_order:'asc',page:1})
    wrapper.getComponent(Pagination).vm.$emit('update:page', 2)
    await flushPromises()
    expect(downstreams.mock.lastCall?.[0]).toMatchObject({sort_by:'cost',sort_order:'asc',page:2})
  })
})
