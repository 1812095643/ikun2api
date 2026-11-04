import { apiClient } from './client'
export interface WarmupWindow { start: string; end: string; rounds: number }
export interface WarmupConfig {
 timezone: string; days: number[]; date?: string; windows: WarmupWindow[]
 account_ids: number[]; group_ids: number[]; exclude_ids: number[]; search: string; plans: string[]
 model: string; count: number; message_gap: number; account_gap: number; timeout: number
 daily_max: number; skip_active: boolean; idle_minutes: number
}
export interface WarmupRule { id: number; name: string; enabled: boolean; priority: number; version: number; config: WarmupConfig }
export interface WarmupAccount {
 id: number; name: string; status: string; schedulable: boolean; plan: string; group_ids: number[]
 rate_limited: boolean; window_end: string | null; last_used_at: string | null
 override: { mode: string; rule_id: number | null }; rule_id: number; rule_name: string
}
export interface WarmupRun {
 id: number; account_id: number; account_name: string; rule_name: string; status: string
 reason: string; planned_at: string; attempts: number; input_tokens: number; output_tokens: number
 snapshot: { before?: { five_hour: number; seven_day: number; checked_at: string; source: string }; results?: Array<{ status: string; window_confirmed: boolean; window_reset?: string }> }
 updated_at: string
}
export interface WarmupState { settings: { enabled: boolean }; rules: WarmupRule[]; accounts: WarmupAccount[]; timezone: string; models: { id: string; display_name: string }[]; groups: { id: number; name: string }[] }
export interface WarmupPreview { accounts: { id: number; name: string; rate_limited: boolean; schedulable: boolean }[]; next: string[]; max_requests: number; quota_check: string }
export function defaultWarmupRule(timezone: string): WarmupRule {
 return { id: 0, name: '', enabled: false, priority: 0, version: 0, config: {
  timezone, days: [0, 1, 2, 3, 4, 5, 6], windows: [{ start: '02:30', end: '04:00', rounds: 1 }],
  account_ids: [], group_ids: [], exclude_ids: [], search: '', plans: [], model: '', count: 2,
  message_gap: 30, account_gap: 60, timeout: 60, daily_max: 6, skip_active: true, idle_minutes: 30
 } }
}
const base = '/admin/workspace/account-warmup'
export const accountWarmupAPI = {
 get: async () => (await apiClient.get<WarmupState>(base)).data,
 settings: async (enabled: boolean) => (await apiClient.put(base + '/settings', { enabled })).data,
 save: async (r: WarmupRule) => r.id ? (await apiClient.put(base + '/rules/' + r.id, r)).data : (await apiClient.post(base + '/rules', r)).data,
 delete: async (id: number) => (await apiClient.delete(base + '/rules/' + id)).data,
 preview: async (id: number) => (await apiClient.post<WarmupPreview>(base + '/rules/' + id + '/preview')).data,
 run: async (id: number, request_id: string) => (await apiClient.post<{ queued: number }>(base + '/rules/' + id + '/run', { request_id, confirm: true })).data,
 overrides: async (ids: number[], mode: string, rule_id: number | null) => (await apiClient.put(base + '/accounts', { ids, mode, rule_id })).data,
 cancel: async () => (await apiClient.post(base + '/cancel')).data,
 runs: async (page: number, page_size: number) => (await apiClient.get<{ items: WarmupRun[]; total: number }>(base + '/runs', { params: { page, page_size } })).data,
}
export const warmupStatus: Record<string, string> = { queued: '等待执行', checking: '主动刷新额度', running: '执行中', success: '请求成功', skipped: '已跳过', cancelled: '已取消', unknown: '结果未知', failed: '失败', rate_limited: '上游限流' }
