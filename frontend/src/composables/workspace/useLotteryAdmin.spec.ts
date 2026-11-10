import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useLotteryAdmin } from './useLotteryAdmin'
import type { LotteryActivity } from '@/api/lottery'
const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn(), save: vi.fn(), publish: vi.fn(), cancel: vi.fn() }))
vi.mock('@/api/lottery', () => ({ lotteryAPI: api }))
vi.mock('@/api/workspace', () => ({ workspaceError: (e: Error) => e.message }))
vi.mock('@/stores/navigation', () => ({ useNavigationStore: () => ({ viewed: vi.fn() }) }))
const activity = () => ({ id: 1, status: 'open', revision: 'current-revision', spec: { title: 'Current rules' } }) as LotteryActivity
function setup() { let state!: ReturnType<typeof useLotteryAdmin>; const wrapper = mount(defineComponent({ setup() { state = useLotteryAdmin(); return () => null } })); return { state, wrapper } }
beforeEach(() => { vi.clearAllMocks(); api.list.mockResolvedValue({ items: [activity()], total: 1 }); api.detail.mockResolvedValue(activity()); api.save.mockResolvedValue({ id: 1, status: 'open', revision: 'new-revision' }) })
describe('进行中抽奖管理', () => {
 it('编辑时重新读取最新规则，保存携带版本且不重新发布', async () => {
  const { state, wrapper } = setup(); await flushPromises()
  await state.detail(1); await state.edit({ ...activity(), revision: 'stale-list' })
  expect(state.selected.value).toBeNull(); expect(state.editing.value?.revision).toBe('current-revision')
  await state.save(activity().spec, false)
  expect(api.save).toHaveBeenCalledWith(activity().spec, 1, 'current-revision')
  expect(api.publish).not.toHaveBeenCalled(); expect(state.message.value).toBe('活动已更新'); expect(state.selected.value?.id).toBe(1)
  wrapper.unmount()
 })
 it('版本冲突保留编辑内容和提示，不静默覆盖', async () => {
  const { state, wrapper } = setup(); await flushPromises(); await state.edit(activity())
  api.save.mockRejectedValue(new Error('活动已更新，请重新打开编辑后保存。'))
  await state.save(activity().spec, false)
  expect(state.editorOpen.value).toBe(true); expect(state.error.value).toContain('重新打开'); expect(api.publish).not.toHaveBeenCalled()
  wrapper.unmount()
 })
 it('活动在打开编辑前已开奖时展示最新结果并拒绝编辑', async () => {
  const { state, wrapper } = setup(); await flushPromises(); api.detail.mockResolvedValue({ ...activity(), status: 'drawn' })
  await state.edit(activity()); expect(state.editorOpen.value).toBe(false); expect(state.selected.value?.status).toBe('drawn')
  wrapper.unmount()
 })
})
