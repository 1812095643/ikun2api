import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { lotteryAPI, type LotteryActivity, type LotteryWin } from '@/api/lottery'
import { workspaceError } from '@/api/workspace'
import { useAuthStore } from '@/stores/auth'
import { useNavigationStore } from '@/stores/navigation'

export function useLottery() {
  const items = ref<LotteryActivity[]>([])
  const wins = ref<LotteryWin[]>([])
  const total = ref(0)
  const winTotal = ref(0)
  const winAmount = ref('0')
  const page = ref(1)
  const winPage = ref(1)
  const tab = ref<'activities' | 'participations' | 'wins'>('activities')
  const historyRevision = ref(0)
  const filter = ref('open')
  const loading = ref(true)
  const busy = ref<number | null>(null)
  const error = ref('')
  const feedback = ref('')
  const detail = ref<LotteryActivity | null>(null)
  const share = ref<LotteryWin | null>(null)
  const now = ref(Date.now())
  const clockOffset = ref(0)
  const joinedID = ref<number | null>(null)
  let alive = true
  let fetching = false
  let timer: ReturnType<typeof setInterval> | undefined
  let feedbackTimer: ReturnType<typeof setTimeout> | undefined
  const settlementAttempts = new Set<number>()
  const auth = useAuthStore()
  const pending = computed(() => wins.value.filter((w) => !w.claimed_at).length)
  function notify(message: string) {
    feedback.value = message
    clearTimeout(feedbackTimer)
    feedbackTimer = setTimeout(() => {
      feedback.value = ''
      joinedID.value = null
    }, 2600)
  }
  async function load(silent = false) {
    if (fetching) return
    fetching = true
    if (!silent) loading.value = true
    try {
      const [activities, records] = await Promise.all([
        lotteryAPI.list(false, page.value, filter.value),
        lotteryAPI.wins(winPage.value)
      ])
      if (!alive) return
      const priorWins = new Set(wins.value.map((w) => w.id))
      if (silent && records.items.some((w) => !priorWins.has(w.id)))
        notify('好运揭晓，你有新的中奖记录')
      let credited = false
      if (busy.value === null) {
        for (let i = 0; i < records.items.length; i++) {
          const win = records.items[i]
          if (win.claimed_at || settlementAttempts.has(win.id)) continue
          settlementAttempts.add(win.id)
          busy.value = win.id
          try {
            const verified = await lotteryAPI.win(win.id)
            records.items[i] = verified.claimed_at ? verified : await lotteryAPI.claim(verified.id)
            credited = true
          } catch {
            notify('这笔奖金暂未入账，可在中奖记录中重试领取')
          } finally {
            busy.value = null
          }
        }
      }
      if (credited) {
        await auth.refreshUser()
        notify('中奖奖金已核验，自动存入你的余额')
      }
      items.value = activities.items
      total.value = activities.total
      wins.value = records.items
      winTotal.value = records.total
      winAmount.value = records.total_amount
      clockOffset.value = new Date(activities.server_time).getTime() - Date.now()
      if (detail.value) {
        const updated = await lotteryAPI.detail(detail.value.id)
        if (alive && detail.value) detail.value = updated
      }
      error.value = ''
      historyRevision.value++
      void useNavigationStore().viewed('/lottery', activities.items.map(item => `${item.status}:${item.id}`))
    } catch (e) {
      if (alive && !silent) error.value = workspaceError(e)
    } finally {
      fetching = false
      if (alive) loading.value = false
    }
  }
  async function join(item: LotteryActivity) {
    if (busy.value) return
    busy.value = item.id
    error.value = ''
    try {
      await lotteryAPI.join(item.id)
      joinedID.value = item.id
      notify('报名成功，祝你好运')
      await load(true)
    } catch (e) {
      error.value = workspaceError(e)
    } finally {
      busy.value = null
    }
  }
  async function claim(win: LotteryWin) {
    if (busy.value) return
    busy.value = win.id
    error.value = ''
    try {
      const verified = await lotteryAPI.win(win.id)
      const result = verified.claimed_at ? verified : await lotteryAPI.claim(verified.id)
      notify(`奖金 $${result.amount} 已存入余额`)
      await Promise.all([load(true), auth.refreshUser()])
    } catch (e) {
      error.value = workspaceError(e)
    } finally {
      busy.value = null
    }
  }
  async function openDetail(id: number) {
    try {
      detail.value = await lotteryAPI.detail(id)
    } catch (e) {
      error.value = workspaceError(e)
    }
  }
  async function openShare(win: LotteryWin) {
    try {
      share.value = await lotteryAPI.win(win.id)
    } catch (e) {
      error.value = workspaceError(e)
    }
  }
  async function changeFilter(status: string) {
    filter.value = status
    page.value = 1
    await load()
  }
  onMounted(() => {
    void load()
    let tick = 0
    timer = setInterval(() => {
      now.value = Date.now() + clockOffset.value
      tick++
      if (tick % 5 === 0 && !document.hidden) void load(true)
    }, 1000)
  })
  onBeforeUnmount(() => {
    alive = false
    clearInterval(timer)
    clearTimeout(feedbackTimer)
  })
  return {
    items,
    wins,
    total,
    winTotal,
    winAmount,
    page,
    winPage,
    tab,
    historyRevision,
    filter,
    loading,
    busy,
    error,
    feedback,
    detail,
    share,
    now,
    joinedID,
    pending,
    load,
    join,
    claim,
    openDetail,
    openShare,
    changeFilter
  }
}
