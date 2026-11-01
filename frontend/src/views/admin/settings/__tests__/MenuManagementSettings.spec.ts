import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import MenuManagementSettings from '../MenuManagementSettings.vue'
import Toggle from '@/components/common/Toggle.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: ref('zh'), t: (key: string) => key }) }))

describe('menu management', () => {
  it('searches canonical pages and preserves unrelated visibility when toggling', async () => {
    const wrapper = mount(MenuManagementSettings, { props: { visibility: { '/news': false }, customItems: [] } })
    await wrapper.get('input[type="search"]').setValue('生图工作室')
    expect(wrapper.findAllComponents(Toggle)).toHaveLength(1)
    expect(wrapper.text()).toContain('生图工作室')
    await wrapper.getComponent(Toggle).trigger('click')
    expect(wrapper.emitted('update:visibility')?.slice(-1)[0]).toEqual([{ '/news': false, '/image-studio': false }])
    expect(wrapper.props('visibility')).toEqual({ '/news': false })
  })

  it('manages custom pages separately and can hide a same-origin page', async () => {
    const item = { id: 'status-page', label: '在线智商检测', url: '/status-detection/', icon_svg: '', visibility: 'user' as const, sort_order: 0 }
    const wrapper = mount(MenuManagementSettings, { props: { visibility: {}, customItems: [item] }, global: { stubs: { ImageUpload: true } } })
    const control = wrapper.get('button[aria-label="在线智商检测 admin.settings.menus.visible"]')
    await control.trigger('click')
    expect(wrapper.emitted('update:visibility')?.slice(-1)[0]).toEqual([{ '/custom/status-page': false }])
    const url = wrapper.findAll('input[type="text"]').find(input => (input.element as HTMLInputElement).value === '/status-detection/')
    expect(url).toBeDefined()
    await url!.setValue('/status-detection/?compact=1')
    expect(wrapper.emitted('update:customItems')?.slice(-1)[0]).toEqual([[{ ...item, url: '/status-detection/?compact=1' }]])
  })
})
