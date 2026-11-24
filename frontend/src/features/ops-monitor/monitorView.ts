import type { MonitorIncident, MonitorNames, MonitorRule } from '@/api/admin/monitor'

export interface ScopePart { kind: string; label: string; title?: string }

const kindLabel: Record<string, string> = { account: '上游账号', group: '分组', user: '用户', key: 'Key', channel: '渠道', model: '模型', proxy: '代理' }
const sourceNames: Record<string, string> = {
  gateway: '网关实时观测', 'postgres.errors': '错误日志', 'postgres.attempts': '上游尝试记录', 'postgres.usage': '用量记录', 'postgres.accounts': '账号状态',
  'postgres.balances': '用户余额', 'postgres.system': '系统日志', 'postgres.payments': '支付订单', 'postgres.tasks': '异步任务', 'local.health': '本机健康检查',
  notification: '通知投递', monitor: '监控自检', state_capacity: '监控存储容量', evaluation: '规则评估', task_evaluation: '任务评估', host: '服务器资源',
}

export function sourceName(id: string): string {
  if (sourceNames[id]) return sourceNames[id]
  const base = id.replace(/\.reconcile$/, '')
  if (base !== id && sourceNames[base]) return `${sourceNames[base]}（对账补采）`
  return id
}

export function nameOf(names: MonitorNames | undefined, kind: string, id?: number | string | null): string {
  if (!id || id === '0') return ''
  const hit = names?.[`${kind}:${id}`]
  if (hit?.name) return hit.name
  if (hit?.email) return hit.email
  return `${kindLabel[kind] || kind} #${id}`
}

/** The user behind a request: the key's owner when the user ID itself was not recorded. */
export function userOf(names: MonitorNames | undefined, userId?: number, keyId?: number): { label: string; email?: string } | null {
  let id = userId
  if (!id && keyId) id = names?.[`key:${keyId}`]?.owner
  if (!id) return null
  const hit = names?.[`user:${id}`]
  return { label: hit?.name || hit?.email || `用户 #${id}`, email: hit?.name ? hit?.email : undefined }
}

/** Turns "account:31/group:23/model:x" into readable labelled parts. */
export function scopeParts(i: Pick<MonitorIncident, 'scope'> & { model?: string }, names?: MonitorNames): ScopePart[] {
  const scope = i.scope || ''
  if (scope === 'digest') return [{ kind: 'digest', label: '每日摘要' }]
  if (scope.startsWith('signature:')) return [{ kind: 'signature', label: '同一会话的思考签名' }]
  if (!scope.includes(':')) return [{ kind: 'source', label: sourceName(scope) }]
  const parts: ScopePart[] = []
  for (const piece of scope.split('/')) {
    const [kind, ...rest] = piece.split(':')
    const value = rest.join(':')
    if (!value || value === '0') continue
    if (kind === 'model') parts.push({ kind, label: value, title: '模型' })
    else if (kind === 'task') parts.push({ kind, label: `任务 ${value.slice(0, 12)}` })
    else if (/^\d+$/.test(value)) parts.push({ kind, label: nameOf(names, kind, value), title: `${kindLabel[kind] || kind} #${value}` })
    else parts.push({ kind, label: `${kindLabel[kind] || kind} ${value}` })
  }
  return parts.length ? parts : [{ kind: 'source', label: scope }]
}

export function incidentTitle(i: MonitorIncident, rules: MonitorRule[], names?: MonitorNames): string {
  if (i.rollup && i.scope.startsWith('account:')) {
    const id = i.scope.split(':')[1]
    return `上游账号「${nameOf(names, 'account', id)}」出现异常`
  }
  if (i.rollup && i.scope.startsWith('group:')) {
    const id = i.scope.split(':')[1]
    return `分组「${nameOf(names, 'group', id)}」突然没有可用账号`
  }
  return rules.find(r => r.id === i.rule_id)?.title || i.explanation?.conclusion || i.rule_id
}

export const severityMeta: Record<string, { label: string; rule: string; pill: string }> = {
  P0: { label: '紧急', rule: 'bg-rose-800', pill: 'bg-rose-800 text-white' },
  P1: { label: '严重', rule: 'bg-red-500', pill: 'bg-red-50 text-red-700 ring-1 ring-inset ring-red-200 dark:bg-red-950/60 dark:text-red-300 dark:ring-red-900' },
  P2: { label: '一般', rule: 'bg-amber-500', pill: 'bg-amber-50 text-amber-800 ring-1 ring-inset ring-amber-200 dark:bg-amber-950/50 dark:text-amber-300 dark:ring-amber-900' },
  P3: { label: '提示', rule: 'bg-slate-300 dark:bg-slate-600', pill: 'bg-slate-100 text-slate-600 ring-1 ring-inset ring-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:ring-slate-700' },
}
export const statusMeta: Record<string, { label: string; cls: string }> = {
  firing: { label: '正在发生', cls: 'text-red-600 dark:text-red-400' },
  candidate: { label: '待确认', cls: 'text-amber-700 dark:text-amber-300' },
  recovering: { label: '恢复观察中', cls: 'text-amber-700 dark:text-amber-300' },
  resolved: { label: '已恢复', cls: 'text-primary-700 dark:text-primary-400' },
  superseded: { label: '规则已更新', cls: 'text-slate-500' },
}

const ts = (v?: string) => (!v || v.startsWith('0001') ? 0 : new Date(v).getTime())
export const hasTime = (v?: string) => ts(v) > 0

/** Time windows the overview can look back over, in minutes. Older faults are not actionable. */
export const recentWindows = [10, 30, 60, 120] as const
export type RecentWindow = (typeof recentWindows)[number]
export const windowLabel = (m: number) => (m < 60 ? `近 ${m} 分钟` : `近 ${m / 60} 小时`)
/** When the incident last failed; falls back to last_seen for incidents without a recorded fault. */
export const faultAt = (i: Pick<MonitorIncident, 'last_fault' | 'last_seen'>) => ts(i.last_fault) || ts(i.last_seen)
export const inWindow = (i: Pick<MonitorIncident, 'last_fault' | 'last_seen'>, now: number, minutes: number) => now - faultAt(i) <= minutes * 60000
export const isOpen = (i: Pick<MonitorIncident, 'status'>) => ['firing', 'candidate', 'recovering'].includes(i.status)
/** Problems the overview reports on: top-level, carded-style incidents (rollup members and digest items are reported elsewhere). */
export const topLevel = (list: MonitorIncident[]) => list.filter(i => !i.parent_id && !i.digest_only && i.status !== 'superseded')
/**
 * State problems (quarantine, pool shrink, paid orders…) do not fail again on their own, so an
 * old one may still be true. Request faults older than the window are no longer actionable.
 */
export function standingProblem(i: MonitorIncident, rules: MonitorRule[], now: number, minutes: number): boolean {
  if (!isOpen(i) || inWindow(i, now, minutes)) return false
  const quiet = rules.find(r => r.id === i.rule_id)?.quiet_close_seconds
  return quiet !== undefined && (quiet === 0 || quiet > 2 * 3600)
}
/** Upstream errors of one account in the admin ops dashboard, where the full error text lives. */
export function accountErrorsLink(accountId: number, status?: string): string {
  const q = new URLSearchParams({ open_error_details: '1', error_type: 'upstream', account_id: String(accountId) })
  if (status && /^\d{3}$/.test(status)) q.set('status_code', status)
  return `/admin/ops?${q.toString()}`
}
/** Upstream accounts named by these incidents, from their scope or sample. */
export function accountsOf(list: MonitorIncident[]): Set<string> {
  const out = new Set<string>()
  for (const i of list) {
    const m = /(?:^|\/)account:(\d+)/.exec(i.scope || '')
    const id = m && m[1] !== '0' ? m[1] : i.sample?.account_id ? String(i.sample.account_id) : ''
    if (id) out.add(id)
  }
  return out
}

export function span(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000))
  if (s < 60) return `${s} 秒`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m} 分钟`
  const h = Math.floor(m / 60)
  if (h < 24) return m % 60 ? `${h} 小时 ${m % 60} 分` : `${h} 小时`
  const d = Math.floor(h / 24)
  return h % 24 ? `${d} 天 ${h % 24} 小时` : `${d} 天`
}
export function ago(v: string | undefined, now = Date.now()): string {
  const t = ts(v)
  if (!t) return '尚无记录'
  const d = now - t
  return d < 45000 ? '刚刚' : `${span(d)}前`
}
export function clock(v: string | undefined): string {
  const t = ts(v)
  if (!t) return '—'
  const d = new Date(t)
  const today = new Date().toDateString() === d.toDateString()
  return d.toLocaleString('zh-CN', today ? { hour: '2-digit', minute: '2-digit', hour12: false } : { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false })
}
export function seconds(ms: number): string {
  if (!ms) return '—'
  return ms < 10000 ? `${(ms / 1000).toFixed(1)} 秒` : `${Math.round(ms / 1000)} 秒`
}

export const errorClassText: Record<string, string> = {
  rate_limited: '飞书限流，稍后重试', network_or_timeout: '网络超时', platform_unavailable: '飞书服务不可用', authentication_or_permission: '凭据或权限不足',
  platform_rejected: '飞书拒收', message_missing: '原卡片已不存在，已改发新卡', recipient_invalid: '收件目标无效', credentials_missing: '通道未配置',
  channel_not_configured: '通道未配置', smtp_transport_or_rejected: '邮件服务器拒收或连接失败', smtp_tls_required: '邮件服务器不支持加密', token_expired: '飞书令牌过期，已刷新',
}
