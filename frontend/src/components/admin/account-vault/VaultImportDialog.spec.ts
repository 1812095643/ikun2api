import { shallowMount,flushPromises } from '@vue/test-utils'
import { beforeEach,describe,it,expect,vi } from 'vitest'
import VaultImportDialog from './VaultImportDialog.vue'
const mocks=vi.hoisted(()=>({preview:vi.fn(),import:vi.fn(),success:vi.fn()}))
vi.mock('@/api/accountVault',async actual=>({...await actual<typeof import('@/api/accountVault')>(),accountVaultAPI:{preview:mocks.preview,import:mocks.import}}))
vi.mock('@/stores/app',()=>({useAppStore:()=>({showSuccess:mocks.success})}))
function mountDialog(){return shallowMount(VaultImportDialog,{props:{show:true,protect:async<T>(fn:()=>Promise<T>)=>fn()},global:{stubs:{BaseDialog:{template:'<div><slot/><slot name="footer"/></div>'}}}})}
beforeEach(()=>{vi.clearAllMocks();mocks.preview.mockResolvedValue({rows:[{line:1,entry:{email:'test@example.test',password:' TEST_ONLY '}}]});mocks.import.mockResolvedValue({inserted:1,updated:0,skipped:0})})
describe('one-step vault import',()=>{
 it('parses and saves with a single submit',async()=>{
  const w=mountDialog();await w.get('textarea').setValue('test@example.test---- TEST_ONLY ')
  await w.get('form').trigger('submit');await flushPromises()
  expect(mocks.preview).toHaveBeenCalledTimes(1);expect(mocks.import).toHaveBeenCalledTimes(1)
  expect(mocks.import.mock.calls[0][1][0].password).toBe(' TEST_ONLY ')
  expect(w.emitted('saved')).toHaveLength(1);expect(w.text()).not.toContain('下一步');w.unmount()
 })
 it('shows parsing errors without writing any rows',async()=>{
  mocks.preview.mockResolvedValue({rows:[{line:3,entry:{},error:'字段不明确'}]})
  const w=mountDialog();await w.get('textarea').setValue('bad input');await w.get('form').trigger('submit');await flushPromises()
  expect(mocks.import).not.toHaveBeenCalled();expect(w.text()).toContain('第 3 行：字段不明确');w.unmount()
 })
 it('retries the exact prepared batch after an interrupted response',async()=>{
  mocks.import.mockRejectedValueOnce(new Error('network disconnected'))
  const w=mountDialog();await w.get('textarea').setValue('test@example.test----pw');await w.get('form').trigger('submit');await flushPromises()
  const first=mocks.import.mock.calls[0]
  await w.get('form').trigger('submit');await flushPromises()
  expect(mocks.preview).toHaveBeenCalledTimes(1);expect(mocks.import.mock.calls[1]).toEqual(first);w.unmount()
 })
})
