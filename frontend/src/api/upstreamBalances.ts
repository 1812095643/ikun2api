import { apiClient } from './client'

export interface BalanceAccount { id: number; name: string; platform: string }
export interface BalanceMonitorConfig {
  kind: 'auto' | 'sub2api' | 'newapi'
  auth_mode: 'auto' | 'modern' | 'legacy'
  new_api_user_id: number
  preferred_account_id: number
  monitor_enabled: boolean
  interval_minutes: number
  threshold: number
  recipients: string[]
}
export interface BalanceReading {
  account_ids: number[]
  account_names: string[]
  kind: string
  scope: string
  balance: number | null
  unit: string
  unlimited: boolean
  wallet_user_id?: number
  error?: string
}
export interface BalanceSnapshot {
  readings: BalanceReading[]
  comparison: string
  balance: number | null
  unit: string
  scope: string
  unlimited: boolean
  successful: number
  total: number
  error?: string
}
export interface UpstreamBalance {
  id: string
  url: string
  name: string
  config: BalanceMonitorConfig
  token_configured: boolean
  accounts: BalanceAccount[]
  key_count: number
  snapshot: BalanceSnapshot
  alert: { low: boolean; last_attempt: string | null; last_sent: string | null; error: string }
  queued: boolean
  checking: boolean
  last_checked_at: string | null
  next_check_at: string | null
}
export interface UpstreamBalanceDraft {
  name: string
  config: BalanceMonitorConfig
  wallet_token?: string
  clear_wallet_token?: boolean
}
const base = '/admin/workspace/upstream-balances'
export const upstreamBalancesAPI = {
  async list(signal?: AbortSignal) {
    return (await apiClient.get<{ items: UpstreamBalance[]; total: number }>(base, { signal })).data
  },
  async save(id: string, draft: UpstreamBalanceDraft) {
    await apiClient.put(`${base}/${id}`, draft)
  },
  async refresh(id?: string) {
    return (await apiClient.post<{ queued: number }>(id ? `${base}/${id}/refresh` : `${base}/refresh`, {})).data
  }
}
export const balanceKindLabel = (kind: string) => ({ auto: '自动识别', sub2api: 'Sub2API', newapi: 'New API' }[kind] || '待识别')
export const balanceScopeLabel = (scope: string) => ({ wallet: '钱包余额', key: 'Key 额度', subscription: '订阅额度' }[scope] || '—')
export const balanceComparisonLabel = (value: string) => ({ single: '单 Key', consistent: '读数一致', different: '读数不同', partial: '部分失败', unverified: '查询失败' }[value] || '未校验')
export function formatUpstreamBalance(value: number | null | undefined, unit: string, unlimited = false) {
  if (unlimited) return '不限额'
  if (typeof value !== 'number' || !Number.isFinite(value)) return '—'
  return `${value.toLocaleString('zh-CN', { maximumFractionDigits: 4 })} ${unit}`.trim()
}
export function formatBalanceTime(value: string | null) {
  if (!value) return '尚未查询'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}
