import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SidebarBadge from '../SidebarBadge.vue'

describe('侧栏提醒徽标', () => {
  it('数量超过 99 时保留完整无障碍说明', () => {
    const wrapper = mount(SidebarBadge, { props: { badge: { count: 120, dot: false, label: '120 条未读消息' }, collapsed: true } })
    expect(wrapper.text()).toBe('99+')
    expect(wrapper.attributes('aria-label')).toBe('120 条未读消息')
    expect(wrapper.classes()).toContain('sidebar-notice-collapsed')
  })
  it('无数量时展示红点，处理后完全隐藏', async () => {
    const wrapper = mount(SidebarBadge, { props: { badge: { count: 0, dot: true, label: '今日未签到' } } })
    expect(wrapper.find('.sidebar-notice-dot').exists()).toBe(true)
    await wrapper.setProps({ badge: { count: 0, dot: false, label: '' } })
    expect(wrapper.find('.sidebar-notice').exists()).toBe(false)
  })
})
