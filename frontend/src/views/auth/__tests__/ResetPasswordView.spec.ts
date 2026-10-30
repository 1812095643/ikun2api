import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ResetPasswordView from '@/views/auth/ResetPasswordView.vue'

const { route, reset, showError } = vi.hoisted(() => ({
  route: { query: {} as Record<string, unknown>, hash: '' },
  reset: vi.fn(), showError: vi.fn()
}))
vi.mock('vue-router', () => ({ useRoute: () => route }))
vi.mock('vue-i18n', () => ({
  createI18n: () => ({ global: { t: (key: string) => key } }),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError, showSuccess: vi.fn() }) }))
vi.mock('@/api/auth', () => ({ resetPassword: (...args: unknown[]) => reset(...args) }))

const token = 'a'.repeat(64)
const mountView = () => mount(ResetPasswordView, { global: { stubs: {
  AuthLayout: { template: '<div><slot/><slot name="footer"/></div>' },
  Icon: true, RouterLink: { template: '<a><slot/></a>' }
} } })
async function fill(wrapper: ReturnType<typeof mountView>, password = 'new-password', confirmation = password) {
  await wrapper.get('#password').setValue(password)
  await wrapper.get('#confirmPassword').setValue(confirmation)
}
describe('Password reset link and form', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    reset.mockResolvedValue({ message: 'ok' })
    route.query = {}
    route.hash = `#email=user%2Btag%40example.com&token=${token}`
    window.history.replaceState({ kept: true }, '', '/reset-password' + route.hash)
  })
  it('reads fragment and removes the secret URL while preserving router state', async () => {
    const wrapper = mountView(); await flushPromises()
    expect(window.location.hash).toBe('')
    expect(window.history.state.kept).toBe(true)
    await fill(wrapper); await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(reset).toHaveBeenCalledWith({ email: 'user+tag@example.com', token, new_password: 'new-password' })
    expect(wrapper.text()).toContain('auth.passwordResetSuccess')
    expect(wrapper.find('#password').exists()).toBe(false)
  })
  it('supports an older query link and clears its query', async () => {
    route.hash = ''; route.query = { email: 'user@example.com', token }
    window.history.replaceState({}, '', `/reset-password?email=user%40example.com&token=${token}`)
    const wrapper = mountView(); await flushPromises()
    expect(window.location.search).toBe('')
    await fill(wrapper); await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(reset.mock.calls[0][0].token).toBe(token)
  })
  it('keeps invalid links out of the form', async () => {
    route.hash = '#email=user@example.com&token=wrong'
    const wrapper = mountView(); await flushPromises()
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.text()).toContain('auth.requestNewResetLink')
  })
  it('does not send mismatched or overlong UTF-8 passwords', async () => {
    const wrapper = mountView(); await flushPromises()
    await fill(wrapper, 'new-password', 'different-password'); await wrapper.get('form').trigger('submit')
    expect(reset).not.toHaveBeenCalled()
    await fill(wrapper, '中'.repeat(25)); await wrapper.get('form').trigger('submit')
    expect(reset).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('auth.passwordMaxBytes')
  })
  it('renders the actual API invalid-token reason in the page', async () => {
    reset.mockRejectedValue({ reason: 'INVALID_RESET_TOKEN', message: 'invalid or expired password reset token' })
    const wrapper = mountView(); await flushPromises(); await fill(wrapper)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('auth.invalidOrExpiredToken')
  })
  it('blocks a second submission while the first is in progress', async () => {
    let finish!: () => void
    reset.mockImplementation(() => new Promise<void>(resolve => { finish = resolve }))
    const wrapper = mountView(); await flushPromises(); await fill(wrapper)
    await wrapper.get('form').trigger('submit'); await wrapper.get('form').trigger('submit')
    expect(reset).toHaveBeenCalledTimes(1)
    finish(); await flushPromises()
  })
})
