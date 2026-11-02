import { apiClient } from './client'

export interface ModelChange { account_id: number; added: string[]; removed: string[] }
export interface ModelMonitorConfig { enabled: boolean; interval_minutes: number; notify: boolean; recipients: string[] }
export interface ModelSummary { enabled: boolean; pending: number; errors: number; queued: boolean; checking: boolean; checked_at: string | null }
export interface ModelAccountState {
  account_id: number; baseline: string[]; current: string[]; ignored_added: string[]; ignored_removed: string[];
  checked_at: string | null; error?: string
}
export interface ModelMonitor {
  config: ModelMonitorConfig; accounts: ModelAccountState[]; revision: number; summary: ModelSummary;
  history: { at: string; action: string; actor_id?: number; changes: ModelChange[] }[];
  notification_error: string
}
const base = '/admin/workspace/upstream-balances'
export const upstreamModelsAPI = {
  async get(id: string, signal?: AbortSignal) { return (await apiClient.get<ModelMonitor>(`${base}/${id}/models`, { signal })).data },
  async save(id: string, revision: number, config: ModelMonitorConfig) { await apiClient.put(`${base}/${id}/models`, { revision, config }) },
  async refresh(id?: string) { return (await apiClient.post<{ queued: number }>(id ? `${base}/${id}/models/refresh` : `${base}/models/refresh`, {})).data },
  async act(id: string, revision: number, action: 'apply' | 'ignore' | 'restore', change: ModelChange, initialize_mapping = false) {
    await apiClient.post(`${base}/${id}/models/actions`, { revision, action, ...change, initialize_mapping })
  }
}
export function pendingModelChanges(account: ModelAccountState | undefined, ignored = false): ModelChange {
  if (!account) return { account_id: 0, added: [], removed: [] }
  if (ignored) return { account_id: account.account_id, added: account.ignored_added || [], removed: account.ignored_removed || [] }
  const baseline = new Set(account.baseline)
  const current = new Set(account.current)
  return {
    account_id: account.account_id,
    added: (account.current || []).filter(id => !baseline.has(id) && !account.ignored_added?.includes(id)),
    removed: (account.baseline || []).filter(id => !current.has(id) && !account.ignored_removed?.includes(id))
  }
}
