import { shallowMount,flushPromises } from '@vue/test-utils'
import { beforeEach,it,expect,vi } from 'vitest'
import PoolDisplaySettings from '../PoolDisplaySettings.vue'
const mocks=vi.hoisted(()=>({get:vi.fn(),save:vi.fn(),groups:vi.fn()}))
vi.mock('@/api/pool',()=>({getPoolDisplaySettings:mocks.get,savePoolDisplaySettings:mocks.save}))
vi.mock('@/api/admin/groups',()=>({getAll:mocks.groups}))
vi.mock('@/stores',()=>({useAppStore:()=>({showSuccess:vi.fn()})}))
vi.mock('vue-i18n',async original=>({...await original<typeof import('vue-i18n')>(),useI18n:()=>({t:(key:string)=>key})}))
beforeEach(()=>{vi.clearAllMocks();mocks.get.mockResolvedValue({pool_user_synthetic_count:0,pool_user_synthetic_banned:0,pool_user_synthetic_platform:'anthropic',pool_user_visible_group_ids:[1]});mocks.groups.mockResolvedValue([{id:1,name:'selected'},{id:2,name:'another'}]);mocks.save.mockResolvedValue({})})
it('reads the saved groups and persists multiple checked groups together',async()=>{
 const w=shallowMount(PoolDisplaySettings,{props:{show:true},global:{stubs:{BaseDialog:{template:'<div><slot/><slot name="footer"/></div>'}}}})
 await flushPromises();const checkboxes=w.findAll('input[type="checkbox"]')
 expect((checkboxes[0].element as HTMLInputElement).checked).toBe(true)
 await checkboxes[1].setValue(true);await w.get('form').trigger('submit');await flushPromises()
 expect(mocks.save).toHaveBeenCalledWith(expect.objectContaining({pool_user_visible_group_ids:[1,2]}));expect(w.emitted('saved')).toHaveLength(1);w.unmount()
})
