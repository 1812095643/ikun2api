import { apiClient } from './client'
import type { RedeemCode } from '@/types'
export interface UniversalSubscription {
 id: number; user_id: number; plan_id?: number; order_id?: number; name: string; user_email?: string; user_name?: string
 status: 'active' | 'scheduled' | 'expired' | 'suspended' | 'revoked'
 starts_at: string; expires_at: string; total_credit_usd: number | null
 daily_limit_usd: number | null; weekly_limit_usd: number | null; monthly_limit_usd: number | null
 total_used_usd: number; daily_used_usd: number; weekly_used_usd: number; monthly_used_usd: number
 daily_window_start?: string; weekly_window_start?: string; monthly_window_start?: string
 daily_reset_at: string; weekly_reset_at: string; monthly_reset_at: string
 held_usd: number; remaining_usd: number; notes: string; version: number
}
export interface UniversalRecord { id: number; action: string; actor_id?: number; reason: string; created_at: string }
export interface UniversalHold { id: string; api_key_id: number; amount: number; state: string; expires_at: string; created_at: string }
export interface UniversalRecords { events: UniversalRecord[]; holds: UniversalHold[] }
export interface UniversalList { items: UniversalSubscription[]; total: number; page: number; page_size: number }
export const universalSubscriptionsAPI = {
 async list(admin = false, params: { page?: number; page_size?: number; user_id?: number } = {}) {
  return (await apiClient.get<UniversalList>(`${admin ? '/admin' : ''}/subscriptions/universal`, { params })).data
 },
 async grant(data: { user_id: number; plan_id: number; operation_id: string; notes: string }) {
  return (await apiClient.post<UniversalSubscription>('/admin/subscriptions/universal/grant', data)).data
 },
 async status(row: UniversalSubscription, status: string, reason: string) {
  await apiClient.post(`/admin/subscriptions/universal/${row.id}/status`, { status, reason, version: row.version })
 },
 async adjust(row: UniversalSubscription, data: { total_credit_usd: number; daily_limit_usd: number; weekly_limit_usd: number; monthly_limit_usd: number; expires_at: string; reason: string }) {
  await apiClient.put(`/admin/subscriptions/universal/${row.id}`, { ...data, version: row.version })
 },
 async records(id: number) { return (await apiClient.get<UniversalRecords>(`/admin/subscriptions/universal/${id}/records`)).data },
 async resolveHold(id: string, reason: string) { await apiClient.post(`/admin/subscriptions/universal/holds/${id}/resolve`, { reason }) },
 async generateCodes(planId: number, count: number, expiresInDays: number) {
  return (await apiClient.post<RedeemCode[]>('/admin/redeem-codes/generate', { type: 'subscription', plan_id: planId, count, ...(expiresInDays > 0 ? { expires_in_days: expiresInDays } : {}) })).data
 }
}
export const subscriptionStatusLabels: Record<UniversalSubscription['status'], string> = { active: '生效中', scheduled: '待生效', expired: '已到期', suspended: '已停用', revoked: '已撤销' }
export const subscriptionMoney = (value: number | null | undefined) => value == null ? '不限' : value.toLocaleString('zh-CN', { maximumFractionDigits: 4 })
export const subscriptionTime = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })

export function universalError(value: unknown, fallback = '暂时无法完成，请重试'): string {
 if (value && typeof value === 'object') {
  const error = value as { message?: unknown; detail?: unknown }
  if (typeof error.message === 'string' && error.message) return error.message
  if (typeof error.detail === 'string' && error.detail) return error.detail
 }
 return fallback
}
