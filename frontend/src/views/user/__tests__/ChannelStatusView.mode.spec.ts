import { describe, expect, it, vi, beforeEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'

const isV1 = vi.fn(() => false)
const route = { query: {} as Record<string, string> }
vi.mock('vue-router', () => ({ useRoute: () => route }))

vi.mock('@/utils/featureFlags', () => ({
  isChannelMonitorV1Mode: () => isV1(),
}))

vi.mock('../ChannelStatusV1View.vue', () => ({
  default: defineComponent({ name: 'ChannelStatusV1View', setup: () => () => h('div', { 'data-testid': 'v1' }) }),
}))
vi.mock('../ChannelStatusV2View.vue', () => ({
  default: defineComponent({ name: 'ChannelStatusV2View', setup: () => () => h('div', { 'data-testid': 'v2' }) }),
}))

vi.mock('../ChannelStatusCardsView.vue', () => ({ default: defineComponent({ name: 'ChannelStatusCardsView', setup: () => () => h('div', { 'data-testid': 'cards' }) }) }))

import ChannelStatusView from '../ChannelStatusView.vue'

describe('ChannelStatusView mode switch', () => {
  beforeEach(() => {
    isV1.mockReset()
    route.query = {}
  })

  it('renders status cards when not in v1 mode', () => {
    isV1.mockReturnValue(false)
    const wrapper = mount(ChannelStatusView)
    expect(wrapper.find('[data-testid="cards"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="v1"]').exists()).toBe(false)
  })

  it('keeps the detailed V2 view available', () => {
    isV1.mockReturnValue(false)
    route.query = { monitor_view: 'details' }
    expect(mount(ChannelStatusView).find('[data-testid="v2"]').exists()).toBe(true)
  })

  it('renders V1 when in v1 mode', () => {
    isV1.mockReturnValue(true)
    const wrapper = mount(ChannelStatusView)
    expect(wrapper.find('[data-testid="v1"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="v2"]').exists()).toBe(false)
  })
})
