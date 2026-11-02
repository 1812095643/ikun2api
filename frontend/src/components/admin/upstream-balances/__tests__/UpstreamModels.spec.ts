import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import UpstreamModelDialog from '../UpstreamModelDialog.vue'
import UpstreamModelSettings from '../UpstreamModelSettings.vue'
import { upstreamModelsAPI, pendingModelChanges, type ModelMonitor } from '@/api/upstreamModels'
import type { UpstreamBalance } from '@/api/upstreamBalances'

vi.mock('@/api/upstreamModels', async original => ({ ...(await original<typeof import('@/api/upstreamModels')>()), upstreamModelsAPI: { get: vi.fn(), save: vi.fn(), refresh: vi.fn(), act: vi.fn() } }))
vi.mock('vue-i18n', async original => ({ ...(await original<typeof import('vue-i18n')>()), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
const data = (): ModelMonitor => ({ config: { enabled: false, interval_minutes: 30, notify: false, recipients: [] }, revision: 4, accounts: [{ account_id: 2, baseline: ['old'], current: ['new'], ignored_added: [], ignored_removed: [], checked_at: '2026-11-03T00:00:00Z' }], summary: { enabled: false, pending: 2, errors: 0, checking: false, queued: false, checked_at: '2026-11-03T00:00:00Z' }, history: [], notification_error: '' })
const item = { id: 'site', name: '上游', accounts: [{ id: 2, name: 'Key A', platform: 'openai' }] } as UpstreamBalance
const dialog = { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' }
describe('upstream model changes', () => {
 it('keeps ignored changes separate from actionable changes', () => {
   const state = data().accounts[0]; state.ignored_added = ['new']
   expect(pendingModelChanges(state).added).toEqual([])
   expect(pendingModelChanges(state, true).added).toEqual(['new'])
   expect(pendingModelChanges(state).removed).toEqual(['old'])
 })
 it('requires a recipient only when notification is enabled', async () => {
   const wrapper = mount(UpstreamModelSettings, { props: { config: data().config, busy: false } })
   await wrapper.get('#model-monitor-notify').trigger('click')
   await wrapper.get('form').trigger('submit')
   expect(wrapper.emitted('save')).toBeUndefined()
   await wrapper.get('textarea').setValue('ops@example.com')
   await wrapper.get('form').trigger('submit')
   expect(wrapper.emitted('save')?.[0][0]).toMatchObject({ notify: true, recipients: ['ops@example.com'] })
 })
 it('selectively applies only after confirmation and supplies the current revision', async () => {
   setActivePinia(createPinia())
   vi.mocked(upstreamModelsAPI.get).mockResolvedValue(data())
   vi.mocked(upstreamModelsAPI.act).mockResolvedValue()
   const wrapper = mount(UpstreamModelDialog, { props: { item }, global: { stubs: { BaseDialog: dialog } } })
   await flushPromises()
   const checkboxes = wrapper.findAll('input[type="checkbox"]')
   await checkboxes[1].setValue(true)
   await wrapper.findAll('button').find(b => b.text() === '应用所选')!.trigger('click')
   expect(upstreamModelsAPI.act).not.toHaveBeenCalled()
   await wrapper.findAll('button').find(b => b.text() === '确认应用 1 项')!.trigger('click')
   await flushPromises()
   expect(upstreamModelsAPI.act).toHaveBeenCalledWith('site', 4, 'apply', { account_id: 2, added: ['new'], removed: [] }, false)
 })
})
