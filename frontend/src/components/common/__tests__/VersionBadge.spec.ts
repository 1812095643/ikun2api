import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh'
import VersionBadge from '../VersionBadge.vue'

const api = vi.hoisted(() => ({
  checkUpdates: vi.fn(), getUpdateStatus: vi.fn(), getVersion: vi.fn(),
  performUpdate: vi.fn(), restartService: vi.fn(), rollback: vi.fn(), getRollbackVersions: vi.fn(),
  auth: { isAdmin: true },
}))
vi.mock('@/api/admin/system', () => api)
vi.mock('@/stores/auth', () => ({ useAuthStore: () => api.auth }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copied: ref(false), copyToClipboard: vi.fn() }) }))

const release = {
  current_version: '1.3.40', latest_version: '1.3.41', has_update: true,
  supports_self_update: true, cached: false, build_type: 'release',
  release_info: { name: '1.3.41', body: '<img src=x onerror=alert(1)>', html_url: 'https://github.com/1812095643/ikun2api/releases/tag/v1.3.41', published_at: '' },
}
const wrappers: ReturnType<typeof mount>[] = []
function render() {
  const wrapper = mount(VersionBadge, {
    props: { version: '1.3.40' },
    global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh } })] },
  })
  wrappers.push(wrapper)
  return wrapper
}
beforeEach(() => {
  vi.clearAllMocks()
  api.auth.isAdmin = true
  api.checkUpdates.mockResolvedValue(release)
  api.getUpdateStatus.mockResolvedValue({ phase: 'idle', active: false })
  api.getRollbackVersions.mockResolvedValue({ versions: [{ version: '1.3.39', published_at: '2026-11-03T10:02:00Z', html_url: '' }] })
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })

describe('original version badge and rollback panel', () => {
  it('omits the view-release link while retaining rollback on the latest version', async () => {
    api.checkUpdates.mockResolvedValue({ ...release, latest_version: '1.3.40', has_update: false })
    const wrapper = render()
    await flushPromises()
    await wrapper.get('button').trigger('click')
    expect(wrapper.text()).not.toContain('查看发布')
    expect(wrapper.find(`a[href="${release.release_info.html_url}"]`).exists()).toBe(false)
    expect(wrapper.text()).toContain('版本回退')
    expect(wrapper.find('button[title="刷新"]').exists()).toBe(true)
  })

  it('checks on mount and shows the original amber update indicator', async () => {
    const wrapper = render()
    await flushPromises()
    expect(api.checkUpdates).toHaveBeenCalledWith(false)
    expect(wrapper.find('.animate-ping').exists()).toBe(true)
    await wrapper.get('button').trigger('click')
    expect(wrapper.text()).toContain('立即更新')
    expect(wrapper.text()).toContain('v1.3.41')
    expect(wrapper.text()).not.toContain('直接断开当前请求')
    expect(wrapper.text()).not.toContain('程序与数据保留恢复备份')
    expect(wrapper.find('img').exists()).toBe(false)
  })

  it('loads recent rollback choices and sends the chosen version', async () => {
    api.checkUpdates.mockResolvedValue({ ...release, latest_version: '1.3.40', has_update: false })
    api.rollback.mockResolvedValue({ need_restart: true })
    const wrapper = render()
    await flushPromises()
    await wrapper.get('button').trigger('click')
    await wrapper.findAll('button').find(button => button.text().includes('版本回退'))!.trigger('click')
    await flushPromises()
    expect(api.getRollbackVersions).toHaveBeenCalledOnce()
    await wrapper.findAll('button').find(button => button.text().includes('v1.3.39'))!.trigger('click')
    await wrapper.findAll('button').find(button => button.text().includes('回退到 v1.3.39'))!.trigger('click')
    await flushPromises()
    expect(api.rollback).toHaveBeenCalledWith('1.3.39')
    expect(wrapper.text()).toContain('回退完成')
    expect(wrapper.text()).toContain('立即重启')
    expect(api.restartService).not.toHaveBeenCalled()
  })

  it('reports an incompatible rollback without restarting', async () => {
    api.checkUpdates.mockResolvedValue({ ...release, latest_version: '1.3.40', has_update: false })
    api.rollback.mockRejectedValue({ response: { data: { message: '目标版本不兼容当前账本' } } })
    const wrapper = render()
    await flushPromises()
    await wrapper.get('button').trigger('click')
    await wrapper.findAll('button').find(button => button.text().includes('版本回退'))!.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('v1.3.39'))!.trigger('click')
    await wrapper.findAll('button').find(button => button.text().includes('回退到 v1.3.39'))!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('目标版本不兼容当前账本')
    expect(api.restartService).not.toHaveBeenCalled()
  })

  it('uses the self-update capability instead of assuming every release build supports updates', async () => {
    api.checkUpdates.mockResolvedValue({ ...release, supports_self_update: false })
    const wrapper = render()
    await flushPromises()
    await wrapper.get('button').trigger('click')
    expect(wrapper.findAll('button').some(button => button.text() === '立即更新')).toBe(false)
    expect(wrapper.text()).toContain('当前部署不支持网页更新')
  })

  it('does not request privileged update endpoints for ordinary users', async () => {
    api.auth.isAdmin = false
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('v1.3.40')
    expect(wrapper.find('button').exists()).toBe(false)
    expect(api.checkUpdates).not.toHaveBeenCalled()
  })
})
