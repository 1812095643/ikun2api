import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OpsErrorDetailsModal from '../OpsErrorDetailsModal.vue'

const { listUpstreamErrors, listRequestErrors } = vi.hoisted(() => ({ listUpstreamErrors: vi.fn(), listRequestErrors: vi.fn() }))
vi.mock('@/api/admin/ops', () => ({ opsAPI: { listUpstreamErrors, listRequestErrors } }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string, p?: Record<string, unknown>) => (p ? `${key}:${JSON.stringify(p)}` : key) }),
}))

const stubs = { BaseDialog: { template: '<div><slot /></div>' }, OpsErrorLogTable: true, Select: true }

async function open(props: Record<string, unknown>) {
  const wrapper = mount(OpsErrorDetailsModal, { props: { show: false, timeRange: '1h', errorType: 'upstream', ...props }, global: { stubs } })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

describe('upstream error list deep link', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listUpstreamErrors.mockResolvedValue({ items: [], total: 0 })
  })

  it('narrows to one account and status and includes connection failures', async () => {
    const wrapper = await open({ accountId: 31, initialStatusCode: 404 })
    const params = listUpstreamErrors.mock.calls.at(-1)![0]
    expect(params).toMatchObject({ account_id: 31, status_codes: '404', time_range: '1h' })
    expect(params.phase).toBeUndefined()
    expect(wrapper.text()).toContain('admin.ops.errorDetails.accountFilter')
    await wrapper.findAll('button').find(b => b.text() === 'admin.ops.errorDetails.clearAccountFilter')!.trigger('click')
    expect(wrapper.emitted('update:accountId')?.[0]).toEqual([null])
  })

  it('keeps the usual upstream phase filter without an account', async () => {
    await open({})
    const params = listUpstreamErrors.mock.calls.at(-1)![0]
    expect(params.account_id).toBeUndefined()
    expect(params.phase).toBe('upstream')
  })
})
