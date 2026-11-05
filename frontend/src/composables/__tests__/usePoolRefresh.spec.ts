import { defineComponent, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { usePoolRefresh } from '../usePoolRefresh'

beforeEach(()=>{vi.useFakeTimers();Object.defineProperty(document,'hidden',{configurable:true,value:false});Object.defineProperty(navigator,'onLine',{configurable:true,value:true})})
afterEach(()=>vi.useRealTimers())
describe('pool automatic refresh',()=>{
  it('polls without overlap, pauses while hidden, resumes and cleans up', async()=>{
    let finish!: (value:boolean)=>void
    const refresh=vi.fn(()=>new Promise<boolean>(resolve=>{finish=resolve}))
    const wrapper=mount(defineComponent({setup(){usePoolRefresh(refresh,()=>false);return ()=>null}}))
    await vi.advanceTimersByTimeAsync(30000);expect(refresh).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(120000);expect(refresh).toHaveBeenCalledTimes(1)
    finish(true);await Promise.resolve()
    Object.defineProperty(document,'hidden',{configurable:true,value:true});document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(60000);expect(refresh).toHaveBeenCalledTimes(1)
    Object.defineProperty(document,'hidden',{configurable:true,value:false});document.dispatchEvent(new Event('visibilitychange'))
    expect(refresh).toHaveBeenCalledTimes(2)
    finish(true);await Promise.resolve();wrapper.unmount()
    await vi.advanceTimersByTimeAsync(120000);expect(refresh).toHaveBeenCalledTimes(2)
  })
  it('backs off failed refreshes and supports pause and offline',async()=>{
    const refresh=vi.fn().mockResolvedValue(false)
    let control!:ReturnType<typeof usePoolRefresh>
    const wrapper=mount(defineComponent({setup(){control=usePoolRefresh(refresh,()=>false);return ()=>null}}))
    await vi.advanceTimersByTimeAsync(30000);expect(refresh).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(30000);expect(refresh).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(30000);expect(refresh).toHaveBeenCalledTimes(2)
    control.interval.value='0';await nextTick()
    await vi.advanceTimersByTimeAsync(120000);expect(refresh).toHaveBeenCalledTimes(2)
    control.interval.value='10';await nextTick()
    Object.defineProperty(navigator,'onLine',{configurable:true,value:false});window.dispatchEvent(new Event('offline'))
    await vi.advanceTimersByTimeAsync(20000);expect(refresh).toHaveBeenCalledTimes(2)
    Object.defineProperty(navigator,'onLine',{configurable:true,value:true});window.dispatchEvent(new Event('online'))
    expect(refresh).toHaveBeenCalledTimes(3);wrapper.unmount()
  })
})
