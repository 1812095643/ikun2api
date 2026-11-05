import { shallowMount,flushPromises } from '@vue/test-utils'
import { beforeEach,describe,it,expect,vi } from 'vitest'
import VaultSecurityToggle from './VaultSecurityToggle.vue'
import Toggle from '@/components/common/Toggle.vue'
const mocks=vi.hoisted(()=>({security:vi.fn(),save:vi.fn(),protect:vi.fn(async(fn:()=>Promise<unknown>)=>fn())}))
vi.mock('@/api/accountVault',()=>({accountVaultAPI:{security:mocks.security,saveSecurity:mocks.save}}))
vi.mock('@/stores/app',()=>({useAppStore:()=>({showSuccess:vi.fn()})}))
beforeEach(()=>{vi.clearAllMocks();mocks.security.mockResolvedValue({two_factor_enabled:true});mocks.save.mockResolvedValue({two_factor_enabled:false})})
describe('vault protection switch',()=>{
 it('loads the actual policy and only changes after a successful protected save',async()=>{
  const w=shallowMount(VaultSecurityToggle,{props:{protect:mocks.protect as any}});await flushPromises()
  expect(w.getComponent(Toggle).props('modelValue')).toBe(true)
  w.getComponent(Toggle).vm.$emit('update:modelValue',false);await flushPromises()
  expect(mocks.protect).toHaveBeenCalledTimes(1);expect(mocks.save).toHaveBeenCalledWith(false)
  expect(w.getComponent(Toggle).props('modelValue')).toBe(false);w.unmount()
 })
 it('keeps the switch unavailable when loading fails',async()=>{
  mocks.security.mockRejectedValue(new Error('offline'))
  const w=shallowMount(VaultSecurityToggle,{props:{protect:mocks.protect as any}});await flushPromises()
  expect(w.getComponent(Toggle).props('modelValue')).toBe(false)
  expect(w.getComponent(Toggle).attributes('disabled')).toBeDefined();expect(mocks.save).not.toHaveBeenCalled();w.unmount()
 })
})

describe('vault default protection',()=>{
 it('starts off and disabled until the policy is loaded',async()=>{
  let finish!: (policy:{two_factor_enabled:boolean})=>void
  mocks.security.mockReturnValue(new Promise(resolve=>{finish=resolve}))
  const w=shallowMount(VaultSecurityToggle,{props:{protect:mocks.protect as any}})
  expect(w.getComponent(Toggle).props('modelValue')).toBe(false)
  expect(w.getComponent(Toggle).attributes('disabled')).toBeDefined()
  finish({two_factor_enabled:false});await flushPromises()
  expect(w.getComponent(Toggle).props('modelValue')).toBe(false)
  expect(mocks.save).not.toHaveBeenCalled();w.unmount()
 })
 it('stays off when enabling is rejected because TOTP is not configured',async()=>{
  mocks.security.mockResolvedValue({two_factor_enabled:false})
  mocks.save.mockRejectedValue({reason:'STEP_UP_TOTP_NOT_ENABLED'})
  const w=shallowMount(VaultSecurityToggle,{props:{protect:mocks.protect as any}});await flushPromises()
  w.getComponent(Toggle).vm.$emit('update:modelValue',true);await flushPromises()
  expect(mocks.save).toHaveBeenCalledWith(true)
  expect(w.getComponent(Toggle).props('modelValue')).toBe(false)
  w.unmount()
 })
})
