import { effectScope, nextTick, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useUsageFilterVisibility } from '../useUsageFilterVisibility'

let storage: Map<string, string>
beforeEach(() => {
  storage = new Map([['auth_user', JSON.stringify({ id: 7 })]])
  vi.stubGlobal('localStorage', { getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => storage.set(key, value) })
})
afterEach(() => vi.unstubAllGlobals())

function create(scope: 'admin' | 'user', mode = ref('usage')) {
  const effects = effectScope()
  const result = effects.run(() => useUsageFilterVisibility(scope, mode))!
  return { ...result, mode, stop: () => effects.stop() }
}

describe('usage filter display preferences', () => {
  it('keeps existing filters visible by default and persists a hidden selection', () => {
    const first = create('user')
    expect(first.isVisible('model')).toBe(true)
    expect(first.isVisible('billing_type')).toBe(true)
    expect(first.toggle('billing_type')).toBe(false)
    first.stop()
    const second = create('user')
    expect(second.isVisible('billing_type')).toBe(false)
    expect(second.isVisible('model')).toBe(true)
    second.reveal('billing_type')
    expect(second.isVisible('billing_type')).toBe(true)
    second.stop()
  })

  it('isolates users, administrator views and tabs', async () => {
    const user = create('user')
    user.toggle('model')
    const admin = create('admin')
    expect(admin.isVisible('model')).toBe(true)
    storage.set('auth_user', JSON.stringify({ id: 8 }))
    const another = create('user')
    expect(another.isVisible('model')).toBe(true)
    user.mode.value = 'errors'
    await nextTick()
    expect(user.isVisible('model')).toBe(true)
    user.toggle('category')
    user.mode.value = 'usage'
    await nextTick()
    expect(user.isVisible('model')).toBe(false)
    expect(user.isVisible('category')).toBe(true)
    user.stop(); admin.stop(); another.stop()
  })

  it('restores defaults and tolerates corrupt or unavailable browser storage', () => {
    storage.set('usage-filter-visibility:user:7:usage', '{invalid')
    const filters = create('user')
    expect(filters.hidden.value).toEqual([])
    filters.toggle('model')
    filters.reset()
    expect(filters.hidden.value).toEqual([])
    vi.stubGlobal('localStorage', { getItem: () => { throw new Error('blocked') }, setItem: () => { throw new Error('blocked') } })
    const blocked = create('admin')
    expect(() => blocked.toggle('account_id')).not.toThrow()
    expect(blocked.isVisible('account_id')).toBe(false)
    filters.stop(); blocked.stop()
  })
})
