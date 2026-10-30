import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import { useNavigationStore } from '../navigation'
import type { NavigationStatus } from '@/api/navigation'

const mock = vi.hoisted(() => ({ status: vi.fn(), read: vi.fn(), models: vi.fn(), announcements: vi.fn() }))
const auth = reactive({ user: { id: 8, role: 'user' }, token: 'test', isAdmin: false })
vi.mock('../auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/api/navigation', () => ({ navigationAPI: { status: mock.status, read: mock.read } }))
vi.mock('@/api/modelPlaza', () => ({ getWorkspaceModels: mock.models }))
vi.mock('@/api', () => ({ announcementsAPI: { list: mock.announcements } }))

function status(patch: Partial<NavigationStatus> = {}): NavigationStatus {
  return { tickets: 3, checkin_needed: true, pending_wins: 1, unopened_boxes: 2, activities: ['open:1'], news: ['1', '2'], reset: ['3'], seen: { pages: ['/tutorial:old'] }, ...patch }
}

beforeEach(() => {
  setActivePinia(createPinia())
  localStorage.clear()
  vi.resetAllMocks()
  auth.user = { id: 8, role: 'user' }
  auth.token = 'test'
  auth.isAdmin = false
  mock.status.mockResolvedValue(status())
  mock.read.mockImplementation(async (_section, keys: string[]) => ({ keys }))
  mock.models.mockResolvedValue({ groups: [{ platform: 'openai', models: [{ name: 'gpt-test' }] }] })
  mock.announcements.mockResolvedValue([{ id: 1 }, { id: 2 }])
  vi.stubGlobal('__NAVIGATION_BUILD_ID__', 'test-build')
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ build: 'test-build', pages: { '/tutorial': 'new' } }) }))
})

describe('侧栏真实提醒', () => {
  it('区分未读数量、未签到和页面更新', async () => {
    const store = useNavigationStore()
    await store.refresh()
    expect(store.badge('/tickets').count).toBe(3)
    expect(store.badge('/news').count).toBe(2)
    expect(store.badge('/model-square').count).toBe(1)
    expect(store.badge('/announcements').count).toBe(2)
    expect(store.badge('/checkin').count).toBe(2)
    expect(store.badge('/checkin').dot).toBe(true)
    expect(store.badge('/tutorial').dot).toBe(true)
  })
  it('查看内容后清除对应未读，真实待办保持提示', async () => {
    const store = useNavigationStore()
    await store.refresh()
    await store.viewed('/news', ['1', '2'])
    await store.viewed('/lottery', ['open:1'])
    await store.markPage('/tutorial')
    expect(store.badge('/news').count).toBe(0)
    expect(store.badge('/lottery').count).toBe(1)
    expect(store.badge('/tutorial').dot).toBe(false)
    expect(store.badge('/tickets').count).toBe(3)
    expect(store.badge('/checkin').dot).toBe(true)
  })
  it('已读保存失败时不消除提醒', async () => {
    const store = useNavigationStore()
    await store.refresh()
    mock.read.mockRejectedValue(new Error('unavailable'))
    await store.viewed('/news', ['1', '2'])
    expect(store.badge('/news').count).toBe(2)
  })
  it('用户切换后不应用旧账号正在返回的结果', async () => {
    let resolve!: (data: NavigationStatus) => void
    mock.status.mockReturnValueOnce(new Promise<NavigationStatus>(done => { resolve = done }))
    const store = useNavigationStore()
    const pending = store.refresh()
    auth.user = { id: 9, role: 'user' }
    store.reset()
    resolve(status({ tickets: 99 }))
    await pending
    expect(store.badge('/tickets').count).toBe(0)
    expect(store.seen).toEqual({})
  })
  it('管理端数量来自管理权限，个人入口仍只显示自己', async () => {
    auth.isAdmin = true
    mock.status.mockImplementation(async (admin: boolean) => status({ tickets: admin ? 10 : 1 }))
    const store = useNavigationStore()
    await store.refresh()
    expect(store.badge('/admin/tickets').count).toBe(10)
    expect(store.badge('/tickets').count).toBe(1)
  })
  it('首次接入建立页面基线，避免所有页面都提示更新', async () => {
    mock.status.mockResolvedValue(status({ seen: {} }))
    const store = useNavigationStore()
    await store.refresh()
    expect(mock.read).toHaveBeenCalledWith('pages', ['/tutorial:new'], false)
    expect(store.badge('/tutorial').dot).toBe(false)
  })
  it('关闭签到时不提示签到，开启后即使访问也不抹掉待签到', async () => {
    mock.status.mockResolvedValue(status({ checkin_needed: false, unopened_boxes: 0 }))
    const store = useNavigationStore()
    await store.refresh()
    expect(store.badge('/checkin')).toMatchObject({ count: 0, dot: false })
    mock.status.mockResolvedValue(status({ checkin_needed: true, unopened_boxes: 0 }))
    await store.refresh(true)
    await store.viewed('/checkin')
    expect(store.badge('/checkin')).toMatchObject({ count: 0, dot: true })
  })

  it('同一账号在另一浏览器恢复服务器已读，内容和页面均不重复提醒', async () => {
    const serverSeen: NavigationStatus['seen'] = { pages: ['/tutorial:old'] }
    mock.status.mockImplementation(async () => status({ seen: { ...serverSeen } }))
    mock.read.mockImplementation(async (section: keyof NavigationStatus['seen'], keys: string[]) => {
      serverSeen[section] = [...new Set([...(serverSeen[section] || []), ...keys])]
      return { keys: serverSeen[section] }
    })
    const first = useNavigationStore()
    await first.refresh()
    await first.viewed('/news', ['1', '2'])
    await first.markPage('/tutorial')
    localStorage.clear()
    setActivePinia(createPinia())
    const second = useNavigationStore()
    await second.refresh()
    expect(second.badge('/news').count).toBe(0)
    expect(second.badge('/tutorial').dot).toBe(false)
  })

  it('保存中断后刷新页面会恢复队列并重试，成功前保留提醒', async () => {
    const first = useNavigationStore()
    await first.refresh()
    mock.read.mockRejectedValueOnce(new Error('network interrupted'))
    await first.viewed('/news', ['1', '2'])
    expect(first.badge('/news').count).toBe(2)
    expect(localStorage.getItem('ikun-navigation-pending:8')).toContain('news')
    setActivePinia(createPinia())
    const second = useNavigationStore()
    await second.refresh()
    expect(second.badge('/news').count).toBe(0)
    expect(localStorage.getItem('ikun-navigation-pending:8')).toBeNull()
  })

  it('已显示内容立即保存，不被较慢的状态请求拖住', async () => {
    let resolve!: (data: NavigationStatus) => void
    mock.status.mockReturnValueOnce(new Promise<NavigationStatus>(done => { resolve = done }))
    const store = useNavigationStore()
    const loading = store.refresh()
    const viewing = store.viewed('/news', ['1', '2'])
    expect(mock.read).toHaveBeenCalledWith('news', ['1', '2'], false)
    resolve(status())
    await Promise.all([loading, viewing])
    expect(store.badge('/news').count).toBe(0)
  })

  it('模型先返回时等待服务端已读记录，不闪出全量未读', async () => {
    let resolve!: (data: NavigationStatus) => void
    mock.status.mockReturnValueOnce(new Promise<NavigationStatus>(done => { resolve = done }))
    const store = useNavigationStore()
    const loading = store.refresh()
    await Promise.resolve()
    expect(store.badge('/model-square')).toMatchObject({ count: 0, dot: false })
    resolve(status({ seen: { models: ['openai:gpt-test'], pages: ['/tutorial:new'] } }))
    await loading
    expect(store.badge('/model-square').count).toBe(0)
    expect(store.badge('/tutorial').dot).toBe(false)
  })

  it('同时查看不同内容时串行合并提交，不丢后来的已读', async () => {
    const store = useNavigationStore()
    await store.refresh()
    let resolve!: (data: { keys: string[] }) => void
    mock.read.mockReturnValueOnce(new Promise<{ keys: string[] }>(done => { resolve = done }))
    const first = store.viewed('/news', ['1'])
    const second = store.viewed('/news', ['2'])
    resolve({ keys: ['1'] })
    await Promise.all([first, second])
    expect(store.badge('/news').count).toBe(0)
    expect(mock.read).toHaveBeenLastCalledWith('news', ['2'], false)
  })

  it('旧轮询晚返回不会覆盖已经确认的已读', async () => {
    const store = useNavigationStore()
    await store.refresh()
    let resolve!: (data: NavigationStatus) => void
    mock.status.mockReturnValueOnce(new Promise<NavigationStatus>(done => { resolve = done }))
    const polling = store.refresh(true)
    await store.markPage('/tutorial')
    resolve(status())
    await polling
    expect(store.badge('/tutorial').dot).toBe(false)
  })

  it('待保存队列按账号隔离，切换用户不提交前一个账号的内容', async () => {
    const first = useNavigationStore()
    await first.refresh()
    mock.read.mockRejectedValueOnce(new Error('network interrupted'))
    await first.viewed('/news', ['1', '2'])
    mock.read.mockClear()
    auth.user = { id: 9, role: 'user' }
    first.reset()
    await first.refresh()
    expect(mock.read).not.toHaveBeenCalled()
    expect(first.badge('/news').count).toBe(2)
  })
})
