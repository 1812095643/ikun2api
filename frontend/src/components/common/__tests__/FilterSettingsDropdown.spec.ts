import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import FilterSettingsDropdown from '../FilterSettingsDropdown.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('filter settings dropdown', () => {
  it('shows checked states, emits choices and supports Escape', async () => {
    const wrapper = mount(FilterSettingsDropdown, { props: { options: [{ key: 'model', label: 'Model' }, { key: 'group', label: 'Group' }], hidden: ['group'] }, attachTo: document.body })
    await wrapper.get('[data-testid="usage-filter-settings"]').trigger('click')
    expect(wrapper.get('[data-testid="usage-filter-toggle-model"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-testid="usage-filter-toggle-group"]').attributes('aria-checked')).toBe('false')
    await wrapper.get('[data-testid="usage-filter-toggle-model"]').trigger('click')
    expect(wrapper.emitted('toggle')).toEqual([['model']])
    await wrapper.get('[data-testid="usage-filter-reset"]').trigger('click')
    expect(wrapper.emitted('reset')).toHaveLength(1)
    await wrapper.get('[role="menu"]').trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('[role="menu"]').exists()).toBe(false)
    expect(document.activeElement).toBe(wrapper.get('button').element)
    wrapper.unmount()
  })

  it('closes when clicking outside', async () => {
    const wrapper = mount(FilterSettingsDropdown, { props: { options: [], hidden: [] }, attachTo: document.body })
    await wrapper.get('button').trigger('click')
    document.body.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[role="menu"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
