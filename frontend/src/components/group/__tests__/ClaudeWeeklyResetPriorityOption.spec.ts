import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ClaudeWeeklyResetPriorityOption from '../ClaudeWeeklyResetPriorityOption.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('Claude weekly reset priority', () => {
  it('allows toggling and restoring the group choice', async () => {
    const wrapper = mount(ClaudeWeeklyResetPriorityOption, { props: { platform: 'anthropic', modelValue: true } })
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[role="switch"]').attributes('aria-label')).toBe('admin.groups.weeklyResetPriority.title')
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[false]])
    await wrapper.setProps({ modelValue: false })
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[false], [true]])
  })

  it.each(['openai', 'gemini', 'antigravity', 'composite'])('is absent for %s groups', (platform) => {
    const wrapper = mount(ClaudeWeeklyResetPriorityOption, { props: { platform, modelValue: true } })
    expect(wrapper.find('[role="switch"]').exists()).toBe(false)
  })
})
