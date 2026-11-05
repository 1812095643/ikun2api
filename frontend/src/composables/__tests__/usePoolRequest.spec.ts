import { effectScope } from 'vue'
import { describe, expect, it } from 'vitest'
import { usePoolRequest } from '../usePoolRequest'

function deferred<T>() { let resolve!: (value:T)=>void, reject!: (reason:unknown)=>void; const promise = new Promise<T>((yes,no)=>{resolve=yes;reject=no}); return {promise,resolve,reject} }
describe('pool request ordering', () => {
  it('never lets an older response or finally overwrite the newest request', async () => {
    const scope=effectScope(), request=scope.run(()=>usePoolRequest<string|null>(null))!
    const a=deferred<string>(), b=deferred<string>()
    let oldSignal: AbortSignal | undefined
    const first=request.run(signal=>{oldSignal=signal;return a.promise})
    const second=request.run(()=>b.promise)
    expect(oldSignal?.aborted).toBe(true)
    a.resolve('account A'); await first
    expect(request.loading.value).toBe(true)
    expect(request.data.value).toBeNull()
    b.resolve('account B'); await second
    expect(request.data.value).toBe('account B')
    expect(request.loading.value).toBe(false)
    scope.stop()
  })
  it('retains last data on failure, clears error after recovery, and cancels on disposal', async () => {
    const scope=effectScope(), request=scope.run(()=>usePoolRequest<string|null>(null))!
    await request.run(async ()=>'known')
    const updated=request.updatedAt.value
    expect(await request.run(async ()=>{throw new Error('offline')})).toBe(false)
    expect(request.data.value).toBe('known');expect(request.updatedAt.value).toBe(updated)
    expect(request.failed.value).toBe(true)
    await request.run(async ()=>'recovered');expect(request.failed.value).toBe(false)
    const pending=deferred<string>();const p=request.run(()=>pending.promise)
    scope.stop();pending.resolve('late');await p
    expect(request.data.value).toBe('recovered')
  })
})
