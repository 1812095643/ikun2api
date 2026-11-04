import { mount } from '@vue/test-utils'
import { describe, it, expect, vi, afterEach } from 'vitest'
import VaultSecretCell from './VaultSecretCell.vue'
const copy = vi.hoisted(() => vi.fn(async () => true))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: copy }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn() }) }))
afterEach(() => { vi.useRealTimers(); copy.mockClear() })
describe('vault secret cell', () => {
 it('loads only on explicit action, copies the exact value and does not display it', async () => {
  const read = vi.fn(async () => ' TEST_ONLY complete value ')
  const w = mount(VaultSecretCell, { props: { id: '1', field: 'password', present: true, read } })
  expect(read).not.toHaveBeenCalled()
  expect(w.text()).not.toContain('TEST_ONLY')
  await w.get('button[aria-label="复制 password"]').trigger('click')
  await vi.waitFor(() => expect(copy).toHaveBeenCalledWith(' TEST_ONLY complete value '))
  expect(w.text()).not.toContain('TEST_ONLY')
  w.unmount()
 })
 it('hides a revealed secret after sixty seconds', async () => {
  vi.useFakeTimers()
  const w = mount(VaultSecretCell, { props: { id: '1', field: 'session_key', present: true, read: vi.fn(async () => 'TEST_ONLY') } })
  await w.get('button[aria-label="查看 session_key"]').trigger('click')
  await Promise.resolve()
  expect(w.text()).toContain('TEST_ONLY')
  await vi.advanceTimersByTimeAsync(60001)
  expect(w.text()).not.toContain('TEST_ONLY')
  w.unmount()
 })
 it('never fetches a missing field', async () => {
  const read = vi.fn(async () => '')
  const w = mount(VaultSecretCell, { props: { id: '1', field: 'password', present: false, read } })
  expect(w.findAll('button')).toHaveLength(0); expect(read).not.toHaveBeenCalled()
 })
})
