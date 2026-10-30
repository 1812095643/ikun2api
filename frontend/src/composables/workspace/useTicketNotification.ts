import { computed, onBeforeUnmount, shallowRef } from 'vue'
import { adminAPI } from '@/api/admin'
import { ticketAPI, workspaceError, type TicketDraft, type TicketNotificationDraft } from '@/api/workspace'
import { useAuthStore } from '@/stores/auth'
import { refreshNavigation } from '@/utils/navigationEvents'
import type { AdminUser } from '@/types'

type Recipient = Pick<AdminUser, 'id' | 'email' | 'username' | 'status'>

export function useTicketNotification() {
  const auth = useAuthStore()
  const recipient = shallowRef<Recipient | null>(null)
  const results = shallowRef<Recipient[]>([])
  const searching = shallowRef(false)
  const busy = shallowRef(false)
  const error = shallowRef('')
  const searchError = shallowRef('')
  const subject = shallowRef('')
  const uncertain = shallowRef(false)
  const request = shallowRef<AbortController | null>(null)
  const options = computed(() => {
    const users = [...(recipient.value ? [recipient.value] : []), ...results.value]
    return [...new Map(users.map(user => [user.id, user])).values()].map(user => ({
      value: user.id, label: `#${user.id} · ${user.email}${user.username ? ` · ${user.username}` : ''}`,
      disabled: user.status !== 'active'
    }))
  })
  let revision = 0
  let active = true
  let pending: TicketNotificationDraft | null = null
  let sentID: number | null = null

  async function search(value: string) {
    request.value?.abort()
    const current = ++revision
    const term = value.trim()
    searchError.value = ''
    if (!term) { results.value = []; searching.value = false; return }
    if (term.length > 200) { searchError.value = '搜索内容最多 200 字'; searching.value = false; return }
    const controller = new AbortController()
    request.value = controller
    searching.value = true
    try {
      const userID = term.replace(/^#/, '')
      if (/^[1-9]\d{0,14}$/.test(userID)) {
        const user = await adminAPI.users.getById(Number(userID))
        if (active && current === revision) results.value = [user]
      } else {
        const page = await adminAPI.users.list(1, 20, { search: term }, { signal: controller.signal })
        if (active && current === revision) results.value = page.items
      }
    } catch (e) {
      if (active && current === revision && !controller.signal.aborted) {
        results.value = []
        searchError.value = workspaceError(e)
      }
    } finally {
      if (active && current === revision) searching.value = false
    }
  }

  function select(value: string | number | boolean | null) {
    if (busy.value || uncertain.value) return
    recipient.value = results.value.find(user => user.id === value) || (recipient.value?.id === value ? recipient.value : null)
  }

  async function submit(message: TicketDraft): Promise<number | null> {
    if (busy.value) return null
    if (sentID) return sentID
    if (!auth.isAdmin || !auth.user) { error.value = '请使用管理员账号发送通知'; return null }
    if (!recipient.value || recipient.value.status !== 'active') { error.value = '请先选择可用的收件用户'; return null }
    if (!subject.value.trim()) { error.value = '请填写通知标题'; return null }
    const userID = auth.user.id
    if (!pending) pending = { ...message, subject: subject.value.trim(), category: 'general', priority: 'normal', user_id: recipient.value.id, request_key: crypto.randomUUID() }
    busy.value = true
    error.value = ''
    try {
      const result = await ticketAPI(true).notify(pending)
      if (!active || auth.user?.id !== userID) return null
      sentID = result.id
      pending = null
      uncertain.value = false
      refreshNavigation()
      return result.id
    } catch (e) {
      if (!active || auth.user?.id !== userID) return null
      const code = (e as { status?: number; response?: { status?: number } }).response?.status ?? (e as { status?: number }).status
      const definitive = !!code && code >= 400 && code < 500 && ![408, 425, 429].includes(code)
      if (definitive) pending = null
      uncertain.value = !definitive
      error.value = definitive ? workspaceError(e) : '暂未确认发送结果，请点击「重试发送」，同一条通知不会重复创建。'
      return null
    } finally {
      if (active) busy.value = false
    }
  }

  onBeforeUnmount(() => { active = false; ++revision; request.value?.abort() })
  return { recipient, options, searching, busy, error, searchError, subject, uncertain, search, select, submit }
}
