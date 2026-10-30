import { onMounted, ref } from 'vue'
import { lotteryAPI, type LotteryActivity, type LotterySpec } from '@/api/lottery'
import { workspaceError } from '@/api/workspace'
import { useNavigationStore } from '@/stores/navigation'
export function useLotteryAdmin() {
  const items = ref<LotteryActivity[]>([]),
    total = ref(0),
    page = ref(1),
    filter = ref(''),
    loading = ref(false),
    busy = ref(false),
    error = ref(''),
    editorOpen = ref(false),
    editing = ref<LotteryActivity | null>(null),
    selected = ref<LotteryActivity | null>(null),
    message = ref('')
  async function load() {
    loading.value = true
    try {
      const data = await lotteryAPI.list(true, page.value, filter.value)
      items.value = data.items
      total.value = data.total
      void useNavigationStore().viewed('/admin/lottery', data.items.map(item => `${item.status}:${item.id}`))
      error.value = ''
    } catch (e) {
      error.value = workspaceError(e)
    } finally {
      loading.value = false
    }
  }
  function create() {
    editing.value = null
    editorOpen.value = true
    error.value = ''
    message.value = ''
  }
  function edit(item: LotteryActivity) {
    editing.value = item
    editorOpen.value = true
    error.value = ''
    message.value = ''
  }
  async function detail(id: number) {
    try {
      selected.value = await lotteryAPI.detail(id, true)
    } catch (e) {
      error.value = workspaceError(e)
    }
  }
  async function save(spec: LotterySpec, publish: boolean) {
    if (busy.value) return
    busy.value = true
    error.value = ''
    try {
      const data = await lotteryAPI.save(spec, editing.value?.id)
      editing.value = await lotteryAPI.detail(data.id, true)
      if (publish) await lotteryAPI.publish(data.id)
      editorOpen.value = false
      message.value = publish ? '活动已发布，用户可以开始报名' : '草稿已保存'
      await load()
    } catch (e) {
      error.value = workspaceError(e)
    } finally {
      busy.value = false
    }
  }
  async function transition(action: 'publish' | 'cancel') {
    if (!selected.value || busy.value) return
    busy.value = true
    try {
      await lotteryAPI[action](selected.value.id)
      selected.value = await lotteryAPI.detail(selected.value.id, true)
      await load()
    } catch (e) {
      error.value = workspaceError(e)
    } finally {
      busy.value = false
    }
  }
  onMounted(load)
  return {
    items,
    total,
    page,
    filter,
    loading,
    busy,
    error,
    editorOpen,
    editing,
    selected,
    message,
    load,
    create,
    edit,
    detail,
    save,
    transition
  }
}
