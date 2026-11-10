import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import UsageLatencyCell from '../UsageLatencyCell.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ cachedPublicSettings: null })
}))

describe('UsageLatencyCell', () => {
  it('shows client-perceived first token and highlights gateway queueing', () => {
    const wrapper = mount(UsageLatencyCell, {
      props: {
        row: {
          output_tokens: 100,
          duration_ms: 9000,
          first_token_ms: 2000,
          client_first_token_ms: 6500,
          gateway_wait_ms: 4200,
          pre_forward_ms: 4500,
          upload_ms: 200,
          account_switch_count: 1
        }
      }
    })
    expect(wrapper.get('[data-testid="usage-first-token"]').text()).toBe('6.50s')
    expect(wrapper.get('[data-testid="usage-gateway-wait"]').text()).toBe('4.20s')
    expect(wrapper.find('[data-testid="usage-upload"]').exists()).toBe(false)
  })

  it('flags slow client uploads separately from gateway queueing', () => {
    const wrapper = mount(UsageLatencyCell, {
      props: { row: { output_tokens: 10, duration_ms: 40000, first_token_ms: 19800, client_first_token_ms: 37974, upload_ms: 18100, gateway_wait_ms: 0, pre_forward_ms: 18174 } }
    })
    expect(wrapper.get('[data-testid="usage-upload"]').text()).toBe('18.10s')
    expect(wrapper.find('[data-testid="usage-gateway-wait"]').exists()).toBe(false)
  })

  it('falls back to upstream first token for old rows and hides small queue waits', () => {
    const wrapper = mount(UsageLatencyCell, {
      props: { row: { output_tokens: 100, duration_ms: 5000, first_token_ms: 1500, gateway_wait_ms: 20 } }
    })
    expect(wrapper.get('[data-testid="usage-first-token"]').text()).toBe('1.50s')
    expect(wrapper.find('[data-testid="usage-gateway-wait"]').exists()).toBe(false)
  })
})
