import { apiClient } from './client'

export type PrizeKey = 'special' | 'first' | 'second' | 'third' | 'consolation'
export type ConditionKind =
  | 'spend_today'
  | 'spend_total'
  | 'recharge_today'
  | 'recharge_total'
  | 'registered_before'
  | 'registered_after'
  | 'balance_min'
  | 'user_ids'
export interface LotteryCondition {
  kind: ConditionKind
  value: string
}
export interface LotteryPrize {
  key: PrizeKey
  amount: string
  count: number
}
export interface LotterySpec {
  title: string
  description: string
  business_date: string
  match: 'all' | 'any'
  conditions: LotteryCondition[]
  prizes: LotteryPrize[]
  draw_mode: 'time' | 'full' | 'either'
  capacity: number
  start_at: string
  draw_at: string | null
}
export interface LotteryWin {
  id: number
  lottery_id: number
  user_id?: number
  title: string
  user_email?: string
  user_name: string
  prize_key: PrizeKey
  amount: string
  created_at: string
  claimed_at: string | null
  redeem_id?: number
  balance_after?: number
  win_count?: number
}
export interface LotteryEvent {
  id: number
  action: string
  actor_id: number | null
  detail: Record<string, unknown>
  created_at: string
}
export interface LotteryActivity {
  id: number
  spec: LotterySpec
  status: 'draft' | 'open' | 'drawn' | 'cancelled'
  created_at: string
  published_at: string | null
  drawn_at: string | null
  entry_count: number
  eligible_count: number
  prize_total: string
  prize_count: number
  joined: boolean
  eligible: boolean
  eligibility_message: string
  my_win?: LotteryWin
  winners?: LotteryWin[]
  events?: LotteryEvent[]
  result_digest: string
}
export interface LotteryPage {
  items: LotteryActivity[]
  total: number
  server_time: string
}
export interface LotteryWinPage {
  items: LotteryWin[]
  total: number
  total_amount: string
}
export interface LotteryPreview {
  eligible_count: number
  prize_total: string
  prize_count: number
}
export interface LotteryEntry {
  id: number
  user_id: number
  email: string
  username: string
  joined_at: string
  prize_key: PrizeKey | ''
  amount: string
  claimed: boolean
}
export interface LotteryEntryPage {
  items: LotteryEntry[]
  total: number
}
export type LotteryParticipationResult = 'pending' | 'won' | 'lost' | 'cancelled'
export type LotteryHistoryFilter = '' | LotteryParticipationResult | 'credited' | 'unclaimed'
export interface LotteryParticipation {
  id: number
  lottery_id: number
  title: string
  activity_status: LotteryActivity['status']
  user_id: number
  email: string
  username: string
  joined_at: string
  drawn_at: string | null
  result: LotteryParticipationResult
  win_id: number | null
  prize_key: PrizeKey | ''
  amount: string
  claimed_at: string | null
  redeem_id: number | null
}
export interface LotteryParticipationPage {
  items: LotteryParticipation[]
  total: number
  page: number
  page_size: number
  summary: {
    entries: number
    pending: number
    won: number
    lost: number
    cancelled: number
    credited: number
    unclaimed: number
    amount: string
    credited_amount: string
  }
}
const user = '/workspace'
const admin = '/admin/workspace'
export const lotteryAPI = {
  async list(manage = false, page = 1, status = '') {
    return (
      await apiClient.get<LotteryPage>(`${manage ? admin : user}/lotteries`, {
        params: { page, page_size: 12, status: status || undefined }
      })
    ).data
  },
  async detail(id: number, manage = false) {
    return (await apiClient.get<LotteryActivity>(`${manage ? admin : user}/lotteries/${id}`)).data
  },
  async join(id: number) {
    return (await apiClient.post(`${user}/lotteries/${id}/join`, {})).data
  },
  async wins(page = 1) {
    return (
      await apiClient.get<LotteryWinPage>(`${user}/lottery-wins`, {
        params: { page, page_size: 20 }
      })
    ).data
  },
  async win(id: number) {
    return (await apiClient.get<LotteryWin>(`${user}/lottery-wins/${id}`)).data
  },
  async claim(id: number) {
    return (await apiClient.post<LotteryWin>(`${user}/lottery-wins/${id}/claim`, { confirm: true }))
      .data
  },
  async preview(spec: LotterySpec) {
    return (await apiClient.post<LotteryPreview>(`${admin}/lotteries/preview`, spec)).data
  },
  async save(spec: LotterySpec, id?: number) {
    return (
      await (id
        ? apiClient.put<{ id: number }>(`${admin}/lotteries/${id}`, spec)
        : apiClient.post<{ id: number }>(`${admin}/lotteries`, spec))
    ).data
  },
  async publish(id: number) {
    return (await apiClient.post(`${admin}/lotteries/${id}/publish`, { confirm: true })).data
  },
  async cancel(id: number) {
    return (await apiClient.post(`${admin}/lotteries/${id}/cancel`, { confirm: true })).data
  },
  async entries(id: number, page = 1) {
    return (
      await apiClient.get<LotteryEntryPage>(`${admin}/lotteries/${id}/entries`, {
        params: { page, page_size: 30 }
      })
    ).data
  },
  async participations(
    manage: boolean,
    params: {
      page: number
      lottery_id?: number
      activity?: string
      search?: string
      result?: LotteryHistoryFilter
    },
    signal?: AbortSignal
  ) {
    return (
      await apiClient.get<LotteryParticipationPage>(`${manage ? admin : user}/lottery-entries`, {
        params: { ...params, page_size: 20 },
        signal
      })
    ).data
  }
}
export const participationNames: Record<LotteryParticipationResult, string> = {
  pending: '等待开奖',
  won: '已中奖',
  lost: '未中奖',
  cancelled: '活动已取消'
}
export const historyFilterOptions: { value: LotteryHistoryFilter; label: string }[] = [
  { value: '', label: '全部结果' },
  { value: 'pending', label: '等待开奖' },
  { value: 'won', label: '已中奖' },
  { value: 'lost', label: '未中奖' },
  { value: 'credited', label: '奖金已到账' },
  { value: 'unclaimed', label: '奖金待领取' },
  { value: 'cancelled', label: '活动已取消' }
]
export const prizeNames: Record<PrizeKey, string> = {
  special: '特等奖',
  first: '一等奖',
  second: '二等奖',
  third: '三等奖',
  consolation: '安慰奖'
}
export const conditionNames: Record<ConditionKind, string> = {
  spend_today: '当日消费大于',
  spend_total: '累计消费大于',
  recharge_today: '当日充值大于',
  recharge_total: '累计充值大于',
  registered_before: '注册早于',
  registered_after: '注册不早于',
  balance_min: '当前余额大于',
  user_ids: '指定用户编号'
}
export const statusNames = { draft: '草稿', open: '报名中', drawn: '已开奖', cancelled: '已取消' }
export function lotteryMoney(value: string | number) {
  return Number(value).toLocaleString('zh-CN', { maximumFractionDigits: 8 })
}
export function lotteryTime(value?: string | null) {
  return value
    ? new Intl.DateTimeFormat('zh-CN', {
        timeZone: 'Asia/Shanghai',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        hour12: false
      }).format(new Date(value))
    : '—'
}
export function lotteryConditionText(c: LotteryCondition) {
  return `${conditionNames[c.kind]} ${c.kind.includes('registered') || c.kind === 'user_ids' ? c.value : `$${lotteryMoney(c.value)}`}`
}
export function lotteryDrawText(s: LotterySpec) {
  return s.draw_mode === 'full'
    ? `满 ${s.capacity} 人自动开奖`
    : s.draw_mode === 'either'
      ? `满 ${s.capacity} 人或 ${lotteryTime(s.draw_at)} 开奖`
      : `${lotteryTime(s.draw_at)} 开奖`
}
