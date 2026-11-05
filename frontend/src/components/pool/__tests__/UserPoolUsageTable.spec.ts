import { mount } from '@vue/test-utils'
import { describe, it, expect, vi } from 'vitest'
import type { UserPoolUsage } from '@/api/pool'
import UserPoolUsageTable from '../UserPoolUsageTable.vue'

vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: { usage_output_rate_mode: 'generation' } }) }))

const row: UserPoolUsage = {
  name: 'abc***ghi@example.test', model: 'claude-opus-5-5', reasoning_effort: 'max',
  inbound_endpoint: '/v1/messages', group_name: 'Selected group', created_at: '2026-11-05T01:00:00Z',
  duration_ms: 10000, first_token_ms: 9999, stream: true, input_tokens: 468, output_tokens: 500,
  cache_read_tokens: 100900, cache_creation_tokens: 281, cache_creation_5m_tokens: 281,
  cache_creation_1h_tokens: 0, image_input_tokens: 0, image_output_tokens: 0
}
const DataTable = {
  props: ['columns', 'data', 'loading'],
  template: '<div><div v-for="(row, index) in data" :key="index"><div v-for="column in columns" :key="column.key" :data-field="column.key"><span>{{ column.label }}</span><slot :name="`cell-${column.key}`" :row="row" :value="row[column.key]" /></div></div><slot v-if="!data.length" name="empty" /></div>'
}

describe('user pool usage table', () => {
  it('renders public request details, the shared token cell and corrected output rate', () => {
    const wrapper = mount(UserPoolUsageTable, { props: { rows: [row], loading: false, failed: false }, global: { stubs: { DataTable } } })
    expect(wrapper.findAll('[data-field]').map(cell => cell.attributes('data-field'))).toEqual([
      'name', 'model', 'reasoning_effort', 'inbound_endpoint', 'group_name', 'stream', 'tokens', 'latency', 'created_at'
    ])
    expect(wrapper.get('[data-field="reasoning_effort"]').text()).toContain('Max')
    expect(wrapper.get('[data-field="inbound_endpoint"]').text()).toContain('/v1/messages')
    expect(wrapper.get('[data-field="group_name"]').text()).toContain('Selected group')
    expect(wrapper.get('[data-field="tokens"]').text()).toContain('100.9K')
    expect(wrapper.get('[data-field="tokens"]').text()).toContain('281')
    expect(wrapper.get('[data-testid="usage-output-rate"]').text()).toBe('50.0')
    expect(wrapper.text()).not.toMatch(/account_id|api_key|cost|费用/)
    wrapper.unmount()
  })

  it('keeps missing historical fields empty and shows network failures', async () => {
    const wrapper = mount(UserPoolUsageTable, { props: { rows: [{ ...row, reasoning_effort: null, inbound_endpoint: null, group_name: '', first_token_ms: null, duration_ms: null }], loading: false, failed: false }, global: { stubs: { DataTable } } })
    expect(wrapper.get('[data-field="inbound_endpoint"]').text()).toContain('—')
    expect(wrapper.get('[data-field="group_name"]').text()).toContain('—')
    expect(wrapper.get('[data-testid="usage-output-rate"]').text()).toBe('—')
    await wrapper.setProps({ rows: [], failed: true })
    expect(wrapper.text()).toContain('admin.pool.networkError')
    wrapper.unmount()
  })
})
