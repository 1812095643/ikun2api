import { shallowMount,flushPromises } from '@vue/test-utils'
import { beforeEach,describe,it,expect,vi } from 'vitest'
import UserPoolPanel from '../UserPoolPanel.vue'
const mocks=vi.hoisted(()=>({list:vi.fn(),usage:vi.fn(),create:vi.fn(),revoke:vi.fn()}))
vi.mock('@/api/pool',()=>({listUserPoolAccounts:mocks.list,listUserPoolUsage:mocks.usage,createPoolShareLink:mocks.create,revokePoolShareLink:mocks.revoke}))
vi.mock('vue-i18n',async original=>({...await original<typeof import('vue-i18n')>(),useI18n:()=>({t:(key:string)=>key})}))
vi.mock('@/composables/useClipboard',()=>({useClipboard:()=>({copyToClipboard:vi.fn(async()=>true)})}))
beforeEach(()=>{vi.clearAllMocks();mocks.list.mockResolvedValue({items:[{name:'masked',platform:'anthropic',status:'active'}],total:1})})
describe('read only user pool',()=>{
 it('loads the current user records into the dedicated usage table',async()=>{
  const items=[{name:'abc***ghi@example.test',model:'claude-opus-5-5',group_name:'Selected group',input_tokens:468,output_tokens:142}]
  mocks.usage.mockResolvedValue({items,total:1,page:1,page_size:20})
  const w=shallowMount(UserPoolPanel);await flushPromises()
  await w.findAll('[role="tab"]')[1].trigger('click');await flushPromises()
  expect(mocks.usage).toHaveBeenCalledWith(1,expect.any(AbortSignal))
  expect(w.findComponent({name:'UserPoolUsageTable'}).props('rows')).toEqual(items)
  w.unmount()
 })
 it('only renders the public columns and retains safe usage navigation without per-account request actions',async()=>{
  const w=shallowMount(UserPoolPanel);await flushPromises()
  expect(w.findComponent({name:'DataTable'}).props('columns').map((c:any)=>c.key)).toEqual(['name','platform','status','capacity','usage_window'])
  expect(w.findAll('[role="tab"]')).toHaveLength(2)
  expect(w.text()).not.toContain('admin.pool.actions.requests');w.unmount()
 })
 it('does not send an ordinary login token to the share endpoint',async()=>{
  const fetch=vi.fn().mockResolvedValue({ok:true,status:200,json:async()=>({data:{items:[],total:0}})})
  vi.stubGlobal('fetch',fetch)
  const w=shallowMount(UserPoolPanel,{props:{shared:true,token:'pool_TEST_ONLY'}});await flushPromises()
  expect(mocks.list).not.toHaveBeenCalled()
  expect(mocks.usage).not.toHaveBeenCalled()
  expect(w.findAll('[role="tab"]')).toHaveLength(0)
  expect(fetch).toHaveBeenCalledWith(expect.stringContaining('/pool-share/accounts'),expect.objectContaining({credentials:'omit',referrerPolicy:'no-referrer',headers:{'X-Pool-Share-Token':'pool_TEST_ONLY'}}))
  expect(w.findAll('button').some(b=>b.text()==='admin.pool.sharePool')).toBe(false)
  w.unmount();vi.unstubAllGlobals()
 })
})
