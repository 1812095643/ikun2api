import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { ref } from 'vue'
import AnnouncementTimeline from '../AnnouncementTimeline.vue'
import AnnouncementBell from '../AnnouncementBell.vue'
import { useAnnouncementStore } from '@/stores/announcements'
import type { UserAnnouncement } from '@/types'

vi.mock('vue-i18n', async original => ({ ...(await original<typeof import('vue-i18n')>()), useI18n: () => ({ t: (key: string) => key, locale: ref('zh-CN') }) }))
enableAutoUnmount(afterEach)
afterEach(() => { document.body.innerHTML = ''; document.body.classList.remove('modal-open') })
const items: UserAnnouncement[] = [
 { id: 2, title: '模型上新', content: '<script>bad()</script>支持 **新模型** <img src="x" onerror="bad()">', notify_mode: 'silent', created_at: '2026-11-02T12:00:00Z', updated_at: '2026-11-02T12:00:00Z' },
 { id: 1, title: '服务更新', content: '第二条公告', notify_mode: 'silent', read_at: '2026-10-01T12:00:00Z', created_at: '2026-10-01T12:00:00Z', updated_at: '2026-10-01T12:00:00Z' }
]
describe('announcement timeline', () => {
 it('groups entries by month, shows status and never renders preview HTML', async () => {
   const wrapper = mount(AnnouncementTimeline, { props: { items, loading: false } })
   expect(wrapper.findAll('h3')).toHaveLength(2)
   expect(wrapper.findAll('.timeline-unread')).toHaveLength(1)
   expect(wrapper.find('script').exists()).toBe(false)
   expect(wrapper.find('img').exists()).toBe(false)
   await wrapper.get('button').trigger('click')
   expect(wrapper.emitted('open')?.[0][0]).toEqual(items[0])
 })
 it('opens an entry, saves its read state and restores the list in one dialog', async () => {
   setActivePinia(createPinia())
   const store = useAnnouncementStore()
   store.announcements = structuredClone(items)
   vi.spyOn(store, 'fetchAnnouncements').mockResolvedValue()
   const read = vi.spyOn(store, 'markAsRead').mockImplementation(async id => { store.announcements.find(a => a.id === id)!.read_at = '2026-11-03T00:00:00Z' })
   const wrapper = mount(AnnouncementBell, { attachTo: document.body })
   await wrapper.get('button').trigger('click')
   await flushPromises()
   const entry = document.body.querySelector<HTMLButtonElement>('.timeline-entry')!
   entry.click(); await flushPromises()
   expect(read).toHaveBeenCalledWith(2)
   expect(document.body.querySelectorAll('[role="dialog"]')).toHaveLength(1)
   expect(document.body.querySelector('.markdown-body script')).toBeNull()
   expect(document.body.querySelector('.markdown-body img')?.getAttribute('onerror')).toBeNull()
   const back = [...document.body.querySelectorAll('button')].find(b => b.textContent?.includes('timeline.back'))!
   back.click(); await flushPromises()
   expect(document.body.querySelector('.timeline-entry')?.classList.contains('is-unread')).toBe(false)
 })
})
