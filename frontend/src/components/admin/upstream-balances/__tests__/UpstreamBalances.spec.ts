import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import UpstreamBalanceEditor from '../UpstreamBalanceEditor.vue'
import UpstreamBalanceTable from '../UpstreamBalanceTable.vue'
import UpstreamBalanceDetails from '../UpstreamBalanceDetails.vue'
import { formatUpstreamBalance, type UpstreamBalance } from '@/api/upstreamBalances'

vi.mock('vue-i18n', async importOriginal => ({ ...(await importOriginal<typeof import('vue-i18n')>()), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
const fixture = (): UpstreamBalance => ({
  id: 'supplier', url: 'https://upstream.example', name: '<script>unsafe</script>',
  config: { kind: 'auto', auth_mode: 'auto', new_api_user_id: 0, preferred_account_id: 0, monitor_enabled: false, interval_minutes: 30, threshold: 10, recipients: [] },
  token_configured: true, accounts: [{ id: 1, name: 'Key A', platform: 'openai' }, { id: 2, name: 'Key B', platform: 'claude' }], key_count: 2,
  snapshot: { readings: [{ account_ids: [1], account_names: ['Key A'], kind: 'sub2api', scope: 'wallet', balance: 5, unit: 'USD', unlimited: false }, { account_ids: [2], account_names: ['Key B'], kind: 'sub2api', scope: 'wallet', balance: 5, unit: 'USD', unlimited: false }], comparison: 'consistent', balance: 5, unit: 'USD', scope: 'wallet', unlimited: false, successful: 2, total: 2 },
  alert: { low: false, last_attempt: null, last_sent: null, error: '' }, queued: false, checking: false, last_checked_at: '2026-10-30T12:00:00Z', next_check_at: null
})
const dialog = { template: '<div><slot /><slot name="footer" /></div>' }
describe('upstream balance management', () => {
  it('retains the saved secret without returning a placeholder token', async () => {
    const item = fixture()
    const wrapper = mount(UpstreamBalanceEditor, { props: { item, saving: false }, global: { stubs: { BaseDialog: dialog } } })
    expect(wrapper.get('input[type="password"]').element.value).toBe('')
    await wrapper.get('form').trigger('submit')
    const draft = wrapper.emitted('save')![0][1] as Record<string, unknown>
    expect(draft.wallet_token).toBeUndefined()
    expect(draft.clear_wallet_token).toBe(false)
    expect(draft.config).toEqual(item.config)
  })
  it('uses account IDs and a deduplicated email list in the submission', async () => {
    const wrapper = mount(UpstreamBalanceEditor, { props: { item: fixture(), saving: false }, global: { stubs: { BaseDialog: dialog } } })
    await wrapper.get('#balance-monitor-enabled').trigger('click')
    await wrapper.get('#balance-monitor-recipients').setValue('admin@example.com\nother@example.com')
    await wrapper.get('#balance-wallet-token').setValue('private-new-token')
    await wrapper.get('form').trigger('submit')
    const draft = wrapper.emitted('save')![0][1] as { wallet_token: string; config: { recipients: string[]; monitor_enabled: boolean } }
    expect(draft.wallet_token).toBe('private-new-token')
    expect(draft.config.recipients).toEqual(['admin@example.com', 'other@example.com'])
    expect(draft.config.monitor_enabled).toBe(true)
    await wrapper.setProps({ item: null })
    await wrapper.setProps({ item: fixture() })
    expect(wrapper.get('input[type="password"]').element.value).toBe('')
  })
  it('rejects invalid recipient input and does not close the form', async () => {
    const wrapper = mount(UpstreamBalanceEditor, { props: { item: fixture(), saving: false }, global: { stubs: { BaseDialog: dialog } } })
    await wrapper.get('#balance-monitor-enabled').trigger('click')
    await wrapper.get('#balance-monitor-recipients').setValue('broken-email')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toBeUndefined()
    expect(wrapper.find('[role="alert"]').text()).toContain('有效的提醒邮箱')
  })
  it('renders the supplied wallet reading once and escapes the supplier name', async () => {
    const wrapper = mount(UpstreamBalanceTable, { props: { items: [fixture()], loading: false, refreshing: null }, global: { stubs: { RouterLink: true } } })
    await flushPromises()
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.text()).toContain('<script>unsafe</script>')
    expect(wrapper.text()).toContain('5 USD')
    expect(wrapper.text()).not.toContain('10 USD')
    const link = wrapper.find('a[target="_blank"]')
    expect(link.attributes('href')).toBe('https://upstream.example')
    expect(link.attributes('rel')).toContain('noopener')
  })
  it('shows separate readings and failures instead of inventing a total', () => {
    const item = fixture()
    item.snapshot.balance = null
    item.snapshot.comparison = 'partial'
    item.snapshot.readings[1].error = '余额接口鉴权失败'
    item.snapshot.readings[1].balance = null
    const wrapper = mount(UpstreamBalanceDetails, { props: { item }, global: { stubs: { BaseDialog: dialog } } })
    expect(wrapper.text()).toContain('Key A')
    expect(wrapper.text()).toContain('Key B')
    expect(wrapper.text()).toContain('余额接口鉴权失败')
    expect(wrapper.text()).toContain('部分失败')
  })
  it('preserves zero and negative wallet balances and labels unlimited quotas', () => {
    expect(formatUpstreamBalance(0, 'USD')).toBe('0 USD')
    expect(formatUpstreamBalance(-2, 'USD')).toBe('-2 USD')
    expect(formatUpstreamBalance(null, 'USD', true)).toBe('不限额')
    expect(formatUpstreamBalance(Number.NaN, 'USD')).toBe('—')
  })
})
