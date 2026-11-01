import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ClaudeDesktopAccessOption from '../ClaudeDesktopAccessOption.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('Claude Desktop group exception', () => {
  it('appears only below an enabled parent restriction and defaults off', async () => {
    const wrapper = mount(ClaudeDesktopAccessOption, { props: { restricted: false, modelValue: false } })
    expect(wrapper.find('[role="switch"]').exists()).toBe(false)
    await wrapper.setProps({ restricted: true })
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.text()).toContain('admin.groups.claudeCode.allowDesktop')
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[true]])
    await wrapper.setProps({ modelValue: true })
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('true')
    await wrapper.setProps({ restricted: false })
    expect(wrapper.find('[role="switch"]').exists()).toBe(false)
  })
})
