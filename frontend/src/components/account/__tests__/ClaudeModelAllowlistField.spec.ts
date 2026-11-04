import { mount } from '@vue/test-utils'
import { describe, it, expect } from 'vitest'
import ClaudeModelAllowlistField from '../ClaudeModelAllowlistField.vue'
describe('Claude OAuth 账号白名单', () => {
 it('preserves selected models while toggling enforcement', async () => {
  const wrapper = mount(ClaudeModelAllowlistField, { props: { modelValue: { enabled: false, models: ['claude-sonnet-4-5'] } }, global: { stubs: { ModelWhitelistSelector: true } } })
  await wrapper.get('input[type="checkbox"]').setValue(true); expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([{ enabled: true, models: ['claude-sonnet-4-5'] }])
 })
 it('warns against an enabled empty allowlist', () => { const wrapper = mount(ClaudeModelAllowlistField, { props: { modelValue: { enabled: true, models: [] } }, global: { stubs: { ModelWhitelistSelector: true } } }); expect(wrapper.get('[role="alert"]').text()).toContain('至少选择一个模型') })
})
