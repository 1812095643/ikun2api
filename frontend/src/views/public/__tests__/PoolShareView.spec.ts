import { shallowMount } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { LocationQuery } from 'vue-router'
import PoolShareView from '../PoolShareView.vue'

const mocks = vi.hoisted(() => ({ route: undefined as unknown as { query: LocationQuery; hash: string } }))
vi.mock('vue-router', () => ({ useRoute: () => mocks.route }))
vi.mock('@/components/pool/UserPoolPanel.vue', () => ({ default: { name: 'UserPoolPanel', props: { shared: Boolean, token: String, initialTab: String }, template: '<section />' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

beforeEach(() => { mocks.route = reactive({ query: {} as LocationQuery, hash: '' }) })

describe('pool share URL compatibility', () => {
  it('uses the portable share token even when the embedding site appends its login token', () => {
    mocks.route.query = { share_token: 'pool_PORTABLE_TEST', token: 'foreign-jwt', user_id: '999', ui_mode: 'embedded', tab: 'usage' }
    const wrapper = shallowMount(PoolShareView)
    expect(wrapper.findComponent({ name: 'UserPoolPanel' }).props()).toMatchObject({ shared: true, token: 'pool_PORTABLE_TEST', initialTab: 'usage' })
    wrapper.unmount()
  })
  it('retains fragment links when Sub2API injects a foreign login JWT', () => {
    mocks.route.query = { token: 'foreign-jwt', ui_mode: 'embedded' }
    mocks.route.hash = '#token=pool_LEGACY_TEST&tab=usage'
    const wrapper = shallowMount(PoolShareView)
    expect(wrapper.findComponent({ name: 'UserPoolPanel' }).props()).toMatchObject({ token: 'pool_LEGACY_TEST', initialTab: 'usage' })
    wrapper.unmount()
  })
  it.each(['accounts', 'usage'])('opens query links on the %s tab', tab => {
    mocks.route.query = { token: 'pool_QUERY_TEST', tab }
    const wrapper = shallowMount(PoolShareView)
    expect(wrapper.findComponent({ name: 'UserPoolPanel' }).props()).toMatchObject({ shared: true, token: 'pool_QUERY_TEST', initialTab: tab })
    wrapper.unmount()
  })
  it.each(['accounts', 'usage'])('retains old fragment links on the %s tab', tab => {
    mocks.route.hash = `#token=pool_LEGACY_TEST&tab=${tab}`
    const wrapper = shallowMount(PoolShareView)
    expect(wrapper.findComponent({ name: 'UserPoolPanel' }).props()).toMatchObject({ token: 'pool_LEGACY_TEST', initialTab: tab })
    wrapper.unmount()
  })
  it('uses an explicit query token instead of a stale fragment token', () => {
    mocks.route.query = { token: 'pool_QUERY_TEST', tab: 'usage' }
    mocks.route.hash = '#token=pool_OLD_TEST&tab=accounts'
    const wrapper = shallowMount(PoolShareView)
    expect(wrapper.findComponent({ name: 'UserPoolPanel' }).props()).toMatchObject({ token: 'pool_QUERY_TEST', initialTab: 'usage' })
    wrapper.unmount()
  })
  it.each([null, '', ['pool_FIRST_TEST', 'pool_SECOND_TEST']])('rejects empty or ambiguous query tokens without legacy fallback', token => {
    mocks.route.query = { token }
    mocks.route.hash = '#token=pool_OLD_TEST'
    const wrapper = shallowMount(PoolShareView)
    expect(wrapper.findComponent({ name: 'UserPoolPanel' }).props('token')).toBe('')
    wrapper.unmount()
  })
  it('updates the owner and tab when navigating between query links', async () => {
    mocks.route.query = { token: 'pool_FIRST_TEST' }
    const wrapper = shallowMount(PoolShareView)
    mocks.route.query = { token: 'pool_SECOND_TEST', tab: 'usage' }
    await wrapper.vm.$nextTick()
    expect(wrapper.findComponent({ name: 'UserPoolPanel' }).props()).toMatchObject({ token: 'pool_SECOND_TEST', initialTab: 'usage' })
    wrapper.unmount()
  })
})
