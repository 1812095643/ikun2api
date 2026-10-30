import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import SystemUpdateDialog from '../SystemUpdateDialog.vue'
import { useSystemUpdate } from '@/composables/useSystemUpdate'
const api = vi.hoisted(() => ({checkUpdates:vi.fn(),performUpdate:vi.fn(),getUpdateStatus:vi.fn(),getVersion:vi.fn(),restartService:vi.fn()}))
vi.mock('@/api/admin/system',()=>api)
const release = {current_version:'1.3.16',latest_version:'1.3.17',has_update:true,supports_self_update:true,cached:false,build_type:'release',release_info:{name:'Next',body:'<img src=x onerror=alert(1)>',html_url:'',published_at:''}}
const dialog = defineComponent({props:['show'],template:'<div v-if="show"><slot/><slot name="footer"/></div>'})
beforeEach(()=>{vi.clearAllMocks();api.checkUpdates.mockResolvedValue(release)})
describe('version update controls',()=>{
 it('loads release details on opening and renders notes as escaped text',async()=>{
  const wrapper=mount(SystemUpdateDialog,{props:{show:false,version:'1.3.16'},global:{stubs:{BaseDialog:dialog}}})
  expect(api.checkUpdates).not.toHaveBeenCalled();await wrapper.setProps({show:true});await flushPromises()
  expect(api.checkUpdates).toHaveBeenCalledOnce();expect(wrapper.text()).toContain('1.3.17');expect(wrapper.find('img').exists()).toBe(false);expect(wrapper.text()).toContain('<img src=x onerror=alert(1)>');wrapper.unmount()
 })
 it('keeps the update action unavailable on unsupported startup modes',async()=>{
  api.checkUpdates.mockResolvedValue({...release,supports_self_update:false})
  const wrapper=mount(SystemUpdateDialog,{props:{show:false},global:{stubs:{BaseDialog:dialog}}});await wrapper.setProps({show:true});await flushPromises()
  expect(wrapper.findAll('button').some(b=>b.text()==='备份并一键更新')).toBe(false);wrapper.unmount()
 })
 it('reports a backend refusal without starting a restart',async()=>{
  api.performUpdate.mockRejectedValue({response:{data:{message:'签名校验失败'}}})
  let update:ReturnType<typeof useSystemUpdate>
  const wrapper=mount(defineComponent({setup(){update=useSystemUpdate();return{}},template:'<div/>'}))
  await update!.update();expect(update!.error.value).toBe('签名校验失败');expect(api.restartService).not.toHaveBeenCalled();wrapper.unmount()
 })
 it('polls asynchronous failures and prevents a second concurrent update',async()=>{
  vi.useFakeTimers();api.performUpdate.mockResolvedValue({started:true,need_restart:false});api.getVersion.mockResolvedValue({version:'1.3.16'});api.getUpdateStatus.mockResolvedValue({phase:'failed',error:'备份失败，已恢复',active:false})
  let update:ReturnType<typeof useSystemUpdate>
  const wrapper=mount(defineComponent({setup(){update=useSystemUpdate();return{}},template:'<div/>'}))
  await update!.check();const pending=update!.update();await flushPromises();await update!.update();expect(api.performUpdate).toHaveBeenCalledOnce();await vi.advanceTimersByTimeAsync(2100);await pending
  expect(update!.phase.value).toBe('failed');expect(update!.error.value).toBe('备份失败，已恢复');expect(api.restartService).not.toHaveBeenCalled();wrapper.unmount();vi.useRealTimers()
 })
})
