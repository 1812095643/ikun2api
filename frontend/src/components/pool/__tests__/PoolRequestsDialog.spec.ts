import { shallowMount, flushPromises } from '@vue/test-utils'
import { describe,expect,it,vi } from 'vitest'
import PoolRequestsDialog from '../PoolRequestsDialog.vue'
import type { PoolAccount } from '@/api/pool'
const requests=vi.hoisted(()=>vi.fn())
vi.mock('@/api/pool',()=>({listPoolAccountRequests:requests}))
vi.mock('vue-i18n',async importOriginal=>({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n:()=>({t:(key:string)=>key}) }))
describe('pool request dialog',()=>{
 it('discards a late response after switching accounts or closing',async()=>{
  const pending:Array<{id:number;resolve:(value:unknown)=>void}>=[]
  requests.mockImplementation((id:number)=>new Promise(resolve=>pending.push({id,resolve})))
  const wrapper=shallowMount(PoolRequestsDialog,{props:{account:{id:1,name:'A'} as PoolAccount,scope:'admin'},global:{stubs:{BaseDialog:{template:'<div><slot /></div>'}}}})
  await wrapper.setProps({account:{id:2,name:'B'} as PoolAccount})
  pending[1].resolve({items:[{id:22,model:'model-B'}],total:1});await flushPromises()
  pending[0].resolve({items:[{id:11,model:'model-A'}],total:1});await flushPromises()
  expect(wrapper.text()).toContain('model-B');expect(wrapper.text()).not.toContain('model-A')
  await wrapper.setProps({account:{id:3} as PoolAccount});await wrapper.setProps({account:null})
  pending[2].resolve({items:[{id:33,model:'model-C'}],total:1});await flushPromises()
  expect(wrapper.text()).not.toContain('model-C');wrapper.unmount()
 })
})
