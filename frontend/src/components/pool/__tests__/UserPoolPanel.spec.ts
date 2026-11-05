import { shallowMount,flushPromises } from '@vue/test-utils'
import { afterEach,beforeEach,describe,it,expect,vi } from 'vitest'
import UserPoolPanel from '../UserPoolPanel.vue'
const mocks=vi.hoisted(()=>({list:vi.fn(),usage:vi.fn(),create:vi.fn(),revoke:vi.fn()}))
vi.mock('@/api/pool',()=>({listUserPoolAccounts:mocks.list,listUserPoolUsage:mocks.usage,createPoolShareLink:mocks.create,revokePoolShareLink:mocks.revoke}))
vi.mock('vue-i18n',async original=>({...await original<typeof import('vue-i18n')>(),useI18n:()=>({t:(key:string)=>key})}))
vi.mock('@/composables/useClipboard',()=>({useClipboard:()=>({copyToClipboard:vi.fn(async()=>true)})}))
beforeEach(()=>{vi.clearAllMocks();mocks.list.mockResolvedValue({items:[{name:'masked',platform:'anthropic',status:'active'}],total:1})})
afterEach(()=>{vi.unstubAllGlobals();vi.useRealTimers()})
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
  const fetch=vi.fn().mockResolvedValue({ok:true,status:200,json:async()=>({code:0,data:{items:[],total:0}})})
  vi.stubGlobal('fetch',fetch)
  const w=shallowMount(UserPoolPanel,{props:{shared:true,token:'pool_TEST_ONLY'}});await flushPromises()
  expect(mocks.list).not.toHaveBeenCalled()
  expect(mocks.usage).not.toHaveBeenCalled()
  expect(w.findAll('[role="tab"]')).toHaveLength(2)
  expect(fetch).toHaveBeenCalledWith(expect.stringContaining('/pool-share/accounts'),expect.objectContaining({credentials:'omit',referrerPolicy:'no-referrer',headers:{'X-Pool-Share-Token':'pool_TEST_ONLY'}}))
  expect(w.findAll('button').some(b=>b.text()==='admin.pool.sharePool')).toBe(false)
  w.unmount()
 })
})

function sharedResponse(items: unknown[] = [], total = items.length) {
 return {ok:true,status:200,json:async()=>({code:0,data:{items,total,page:1,page_size:20}})}
}
function pendingResponse() {
 let resolve!: (value: ReturnType<typeof sharedResponse> | {ok: boolean; status: number}) => void
 const promise = new Promise<ReturnType<typeof sharedResponse> | {ok: boolean; status: number}>(finish=>{resolve=finish})
 return {promise,resolve}
}

describe('shared pool usage',()=>{
 it('switches tabs, paginates and refreshes using only the share token',async()=>{
  const items=[{name:'abc***ghi@example.test',model:'claude-opus-5-5',input_tokens:468,output_tokens:142}]
  const fetch=vi.fn().mockImplementation(async(url:string)=>sharedResponse(url.includes('/usage')?items:[],41))
  vi.stubGlobal('fetch',fetch)
  const w=shallowMount(UserPoolPanel,{props:{shared:true,token:'pool_TEST_ONLY'}});await flushPromises()
  await w.findAll('[role="tab"]')[1].trigger('click');await flushPromises()
  expect(w.findComponent({name:'UserPoolUsageTable'}).props('rows')).toEqual(items)
  expect(fetch).toHaveBeenLastCalledWith(expect.stringContaining('/pool-share/usage?page=1'),expect.objectContaining({headers:{'X-Pool-Share-Token':'pool_TEST_ONLY'}}))
  w.findComponent({name:'Pagination'}).vm.$emit('update:page',2);await flushPromises()
  expect(fetch).toHaveBeenLastCalledWith(expect.stringContaining('/pool-share/usage?page=2'),expect.anything())
  await w.findAll('button').find(b=>b.text()==='common.refresh')!.trigger('click');await flushPromises()
  expect(fetch).toHaveBeenLastCalledWith(expect.stringContaining('/pool-share/usage?page=2'),expect.anything())
  expect(mocks.list).not.toHaveBeenCalled();expect(mocks.usage).not.toHaveBeenCalled()
  w.unmount()
 })
 it('can open directly on usage and poll without fetching the account list',async()=>{
  vi.useFakeTimers();Object.defineProperty(document,'hidden',{configurable:true,value:false});Object.defineProperty(navigator,'onLine',{configurable:true,value:true})
  const fetch=vi.fn().mockResolvedValue(sharedResponse())
  vi.stubGlobal('fetch',fetch)
  const w=shallowMount(UserPoolPanel,{props:{shared:true,token:'pool_TEST_ONLY',initialTab:'usage'}});await flushPromises()
  expect(fetch).toHaveBeenCalledTimes(1)
  expect(fetch).toHaveBeenLastCalledWith(expect.stringContaining('/pool-share/usage'),expect.anything())
  await vi.advanceTimersByTimeAsync(30000)
  expect(fetch).toHaveBeenCalledTimes(2)
  expect(fetch.mock.calls.every(([url])=>url.includes('/pool-share/usage'))).toBe(true)
  w.unmount()
  await vi.advanceTimersByTimeAsync(60000);expect(fetch).toHaveBeenCalledTimes(2)
 })
 it('clears old owner data and ignores late responses after changing links',async()=>{
  const old=pendingResponse(),fresh=pendingResponse()
  const fetch=vi.fn().mockResolvedValueOnce(sharedResponse([{model:'old-owner'}],41)).mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise)
  vi.stubGlobal('fetch',fetch)
  const w=shallowMount(UserPoolPanel,{props:{shared:true,token:'pool_OLD',initialTab:'usage'}});await flushPromises()
  w.findComponent({name:'Pagination'}).vm.$emit('update:page',2);await flushPromises()
  await w.setProps({token:'pool_NEW'});await flushPromises()
  expect(w.findComponent({name:'UserPoolUsageTable'}).props('rows')).toEqual([])
  expect(fetch).toHaveBeenLastCalledWith(expect.stringContaining('/usage?page=1'),expect.objectContaining({headers:{'X-Pool-Share-Token':'pool_NEW'}}))
  fresh.resolve(sharedResponse([{model:'new-owner'}]));await flushPromises()
  old.resolve({ok:false,status:401});await flushPromises()
  expect(w.findComponent({name:'UserPoolUsageTable'}).props('rows')).toEqual([{model:'new-owner'}])
  expect(w.find('[role="alert"]').exists()).toBe(false)
  expect(fetch.mock.calls[1][1].signal.aborted).toBe(true)
  w.unmount()
 })
 it('clears records when a link is revoked or expires during refresh',async()=>{
  const fetch=vi.fn().mockResolvedValueOnce(sharedResponse([{model:'private-record'}])).mockResolvedValueOnce({ok:false,status:401})
  vi.stubGlobal('fetch',fetch)
  const w=shallowMount(UserPoolPanel,{props:{shared:true,token:'pool_TEST_ONLY',initialTab:'usage'}});await flushPromises()
  await w.findAll('button').find(b=>b.text()==='common.refresh')!.trigger('click');await flushPromises()
  expect(w.findComponent({name:'UserPoolUsageTable'}).exists()).toBe(false)
  expect(w.find('[role="alert"]').text()).toBe('admin.pool.shareInvalid')
  expect(w.findAll('[role="tab"]').every(b=>b.attributes('disabled')!==undefined)).toBe(true)
  w.unmount()
 })
 it('shows network failure and allows retry without losing the link',async()=>{
  const fetch=vi.fn().mockResolvedValueOnce({ok:false,status:503}).mockResolvedValueOnce(sharedResponse([{model:'recovered'}]))
  vi.stubGlobal('fetch',fetch)
  const w=shallowMount(UserPoolPanel,{props:{shared:true,token:'pool_TEST_ONLY',initialTab:'usage'}});await flushPromises()
  expect(w.findComponent({name:'PoolNetworkError'}).exists()).toBe(true)
  w.findComponent({name:'PoolNetworkError'}).vm.$emit('retry');await flushPromises()
  expect(w.findComponent({name:'UserPoolUsageTable'}).props('rows')).toEqual([{model:'recovered'}])
  expect(w.find('[role="alert"]').exists()).toBe(false)
  w.unmount()
 })
 it('does not request data or fall back to logged in usage when a share token is missing',async()=>{
  const fetch=vi.fn();vi.stubGlobal('fetch',fetch)
  const w=shallowMount(UserPoolPanel,{props:{shared:true,initialTab:'usage'}});await flushPromises()
  expect(fetch).not.toHaveBeenCalled();expect(mocks.usage).not.toHaveBeenCalled()
  expect(w.find('[role="alert"]').text()).toBe('admin.pool.shareInvalid')
  w.unmount()
 })
 it('includes the current usage tab in a generated share link',async()=>{
  mocks.usage.mockResolvedValue({items:[],total:0})
  mocks.create.mockResolvedValue({token:'pool_TEST_ONLY',expires_at:'2026-12-05T10:00:00Z'})
  const w=shallowMount(UserPoolPanel,{global:{stubs:{BaseDialog:{template:'<div><slot/><slot name="footer"/></div>'}}}});await flushPromises()
  await w.findAll('[role="tab"]')[1].trigger('click');await flushPromises()
  await w.findAll('button').find(b=>b.text()==='admin.pool.generateShare')!.trigger('click');await flushPromises()
  const link=new URL((w.find('#pool-share-url').element as HTMLInputElement).value)
  expect(link.pathname).toBe('/share/pool');expect(link.search).toBe('')
  expect(new URLSearchParams(link.hash.slice(1)).get('tab')).toBe('usage')
  expect(new URLSearchParams(link.hash.slice(1)).get('token')).toBe('pool_TEST_ONLY')
  w.unmount()
 })
})
