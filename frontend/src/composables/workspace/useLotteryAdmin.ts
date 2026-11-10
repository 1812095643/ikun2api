import { onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { lotteryAPI, type LotteryActivity, type LotterySpec } from '@/api/lottery'
import { workspaceError } from '@/api/workspace'
import { useNavigationStore } from '@/stores/navigation'

export function useLotteryAdmin() {
  const items = ref<LotteryActivity[]>([])
  const total = shallowRef(0), page = shallowRef(1), filter = shallowRef('')
  const loading = shallowRef(false), detailLoading = shallowRef(false), busy = shallowRef(false)
  const error = shallowRef(''), message = shallowRef(''), editorOpen = shallowRef(false)
  const editing = shallowRef<LotteryActivity | null>(null), selected = shallowRef<LotteryActivity | null>(null)
  const now = shallowRef(Date.now())
  let alive = true, listRevision = 0, detailRevision = 0, returnToDetail = 0
  let timer: ReturnType<typeof setInterval> | undefined

  async function load() {
    const revision = ++listRevision
    loading.value = true
    try {
      const data = await lotteryAPI.list(true, page.value, filter.value)
      if (!alive || revision !== listRevision) return
      items.value = data.items
      total.value = data.total
      void useNavigationStore().viewed('/admin/lottery', data.items.map(item => `${item.status}:${item.id}`))
      error.value = ''
    } catch (e) {
      if (alive && revision === listRevision) error.value = workspaceError(e)
    } finally {
      if (alive && revision === listRevision) loading.value = false
    }
  }
  function create() {
    if (busy.value || detailLoading.value) return
    returnToDetail = 0
    selected.value = null
    editing.value = null
    editorOpen.value = true
    error.value = ''
    message.value = ''
  }
  async function edit(item: LotteryActivity) {
    if (busy.value || detailLoading.value) return
    detailLoading.value = true
    error.value = ''
    try {
      const latest = await lotteryAPI.detail(item.id, true)
      if (!alive) return
      if (!['draft', 'open'].includes(latest.status)) {
        selected.value = latest
        error.value = '活动已开奖或取消，不能修改规则。'
        return
      }
      returnToDetail = selected.value?.id || 0
      selected.value = null
      editing.value = latest
      editorOpen.value = true
      message.value = ''
    } catch (e) {
      if (alive) error.value = workspaceError(e)
    } finally {
      if (alive) detailLoading.value = false
    }
  }
  async function detail(id: number) {
    if (busy.value) return
    const revision = ++detailRevision
    detailLoading.value = true
    try {
      const item = await lotteryAPI.detail(id, true)
      if (alive && revision === detailRevision) { selected.value = item; error.value = '' }
    } catch (e) {
      if (alive && revision === detailRevision) error.value = workspaceError(e)
    } finally {
      if (alive && revision === detailRevision) detailLoading.value = false
    }
  }
  async function closeEditor() {
    if (busy.value) return
    editorOpen.value = false
    error.value = ''
    const id = returnToDetail
    returnToDetail = 0
    if (id) await detail(id)
  }
  async function save(spec: LotterySpec, publish: boolean) {
    if (busy.value) return
    busy.value = true
    error.value = ''
    const updating = editing.value?.status === 'open'
    try {
      const data = await lotteryAPI.save(spec, editing.value?.id, editing.value?.revision)
      // If publication fails, retries must update this saved draft rather than create another one.
      editing.value = { ...editing.value, id: data.id, spec, status: data.status, revision: data.revision } as LotteryActivity
      if (publish && !updating) await lotteryAPI.publish(data.id)
      editorOpen.value = false
      message.value = updating ? '活动已更新' : publish ? '活动已发布' : '草稿已保存'
      await load()
      if (returnToDetail) {
        selected.value = await lotteryAPI.detail(data.id, true)
        returnToDetail = 0
      }
    } catch (e) {
      error.value = workspaceError(e)
    } finally {
      busy.value = false
    }
  }
  async function transition(action: 'publish' | 'cancel') {
    if (!selected.value || busy.value) return
    busy.value = true
    error.value = ''
    try {
      await lotteryAPI[action](selected.value.id)
      selected.value = await lotteryAPI.detail(selected.value.id, true)
      message.value = action === 'publish' ? '活动已发布' : '活动已取消'
      await load()
    } catch (e) {
      error.value = workspaceError(e)
    } finally {
      busy.value = false
    }
  }
  onMounted(() => { void load(); timer = setInterval(() => { now.value = Date.now() }, 1000) })
  onBeforeUnmount(() => { alive = false; listRevision++; detailRevision++; clearInterval(timer) })
  return { items, total, page, filter, loading, detailLoading, busy, error, editorOpen, editing, selected, message, now, load, create, edit, detail, closeEditor, save, transition }
}
