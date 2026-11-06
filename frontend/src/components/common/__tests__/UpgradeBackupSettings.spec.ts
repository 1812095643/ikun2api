import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import UpgradeBackupSettings from '../UpgradeBackupSettings.vue'
const api = vi.hoisted(() => ({ getUpgradeBackups: vi.fn(), configureUpgradeBackups: vi.fn() }))
vi.mock('@/api/admin/system', () => api)
const initial = { policy: { auto_cleanup: true, keep: 3 }, count: 6, complete_count: 5, protected_count: 1, bytes: 1024 }

beforeEach(() => {
  vi.clearAllMocks()
  api.getUpgradeBackups.mockResolvedValue(initial)
  api.configureUpgradeBackups.mockImplementation(async policy => ({ ...initial, policy }))
})

describe('upgrade backup retention controls', () => {
  it('shows persisted settings and sends the chosen policy', async () => {
    const wrapper = mount(UpgradeBackupSettings)
    await flushPromises()
    expect(wrapper.text()).toContain('6 份')
    expect(wrapper.get<HTMLInputElement>('input[type="checkbox"]').element.checked).toBe(true)
    expect(wrapper.get<HTMLInputElement>('input[type="number"]').element.value).toBe('3')
    await wrapper.get('input[type="checkbox"]').setValue(false)
    await wrapper.get('input[type="number"]').setValue('5')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.configureUpgradeBackups).toHaveBeenCalledWith({ auto_cleanup: false, keep: 5 })
    expect(wrapper.text()).toContain('已保存')
    wrapper.unmount()
  })

  it('keeps settings unavailable on a failed load and allows retry', async () => {
    api.getUpgradeBackups.mockRejectedValueOnce(new Error('网络异常'))
    const wrapper = mount(UpgradeBackupSettings)
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('网络异常')
    expect(wrapper.find('form').exists()).toBe(false)
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.find('form').exists()).toBe(true)
    wrapper.unmount()
  })

  it('refuses invalid counts and disables changes during an update', async () => {
    const wrapper = mount(UpgradeBackupSettings)
    await flushPromises()
    await wrapper.get('input[type="number"]').setValue('31')
    await wrapper.get('form').trigger('submit')
    expect(api.configureUpgradeBackups).not.toHaveBeenCalled()
    await wrapper.setProps({ disabled: true })
    expect(wrapper.get('input[type="checkbox"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('input[type="number"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('prevents concurrent saves and preserves errors', async () => {
    let rejectSave!: (error: Error) => void
    api.configureUpgradeBackups.mockReturnValue(new Promise((_resolve, reject) => { rejectSave = reject }))
    const wrapper = mount(UpgradeBackupSettings)
    await flushPromises()
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')
    expect(api.configureUpgradeBackups).toHaveBeenCalledOnce()
    rejectSave(new Error('保存失败'))
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('保存失败')
    expect(wrapper.text()).not.toContain('已保存')
    wrapper.unmount()
  })
})