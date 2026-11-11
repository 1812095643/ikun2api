import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UsageTokenCounts from './UsageTokenCounts.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
describe('OpenAI 缓存写入周期', () => {
  it('共享 Token 列显示 30m/12h，并在标题中保留各自数量', () => {
    const wrapper = mount(UsageTokenCounts, { props: { row: { input_tokens: 1, output_tokens: 2, cache_read_tokens: 0, cache_creation_tokens: 2399, cache_creation_5m_tokens: 0, cache_creation_1h_tokens: 0, cache_creation_30m_tokens: 9, cache_creation_12h_tokens: 2390, image_input_tokens: 0, image_output_tokens: 0 } }, global: { stubs: { Icon: true } } })
    expect(wrapper.text()).toContain('30m')
    expect(wrapper.text()).toContain('12h')
    expect(wrapper.text()).not.toContain('1h')
    const title = wrapper.findAll('[title]').map(el => el.attributes('title')).join(' ')
    expect(title).toContain('30m: 9')
    expect(title).toContain('12h: 2,390')
    wrapper.unmount()
  })
})
