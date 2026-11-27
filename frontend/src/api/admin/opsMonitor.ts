import { apiClient } from '../client'

export interface MonitorSample { request_id?: string; account_id?: number; user_id?: number; group_id?: number; request_started_at?: string; request_ended_at?: string; first_output_at?: string; request_outcome?: string }
export interface MonitorExplanation { conclusion: string; confidence: string; facts: string[]; inferences: string[]; missing_evidence: string[]; advice: string[] }
export interface MonitorIncident {
  id: string; rule_id: string; status: string; severity: string; revision: number; platform?: string; group_id?: number; model?: string
  first_seen: string; last_seen: string; last_fault: string; resolved_at?: string; count: number; request_count: number; account_count: number; user_count: number
  acknowledged: boolean; silenced_until?: string; closed_by?: string; source_quality: string; explanation: MonitorExplanation; sample?: MonitorSample; control_reason?: string; last_actor?: string
  group_name?: string; sample_account_name?: string; scope?: string; quality?: string; last_control_at?: string; recovering_since?: string; healthy_samples?: number
}
export interface MonitorDelivery { id: string; incident_id: string; revision: number; channel: string; target: string; kind: string; state: string; attempts: number; next_attempt: string; accepted_at?: string; error_class?: string; message_id?: string; channel_name?: string; target_name?: string }
export interface MonitorOutcomes { success: number; failed: number; cancelled: number; unknown: number }
export interface MonitorRelatedRequest { request_id: string; outcome: string; account_id: number; account_name: string; group_id: number; group_name: string; started_at?: string; ended_at?: string; client_status: number }
export interface MonitorRelatedAccount { id: number; name: string; deleted: boolean; cursor: string }
export interface MonitorHistory extends MonitorIncident { transition_id: number; recorded_at: string; action: string; actor?: string; actor_name?: string; previous_status?: string }
export interface MonitorRecordPage<T> { items: T[]; has_more: boolean; after_id: string }
export interface MonitorHistoryPage { items: MonitorHistory[]; has_more: boolean; before_id: number }
export interface MonitorRule { id: string; title: string; advice: string; recovery_samples: number; recovery_seconds: number }
export interface MonitorProfile { queue_warn_seconds: number; queue_fail_seconds: number; first_warn_seconds: number; first_fail_seconds: number; stall_warn_seconds: number; stall_fail_seconds: number; task_sla_seconds: number }
export interface NotificationPolicy {
  id: string; name: string; enabled: boolean; rules: string[]; platforms: string[]; group_ids: number[]; account_ids: number[]; models: string[]; severities: string[]; kinds: string[]
  target_ids: string[]; fallback_target_ids: string[]; repeat_minutes: number; mention_all: boolean
}
export interface MonitorConfig {
  version: number; shadow: boolean; site_id: string; site_name?: string; retention_days: number; detail_base_url: string
  rules: Record<string, { enabled: boolean; notification_owner: 'legacy' | 'monitor'; version: number }>
  profiles: Record<string, MonitorProfile>; balance_low_usd?: number; subscription_low_ratio?: number; payment_grace_seconds: number; billing_grace_seconds: number
  policies: NotificationPolicy[]; owner_overrides: { rule_id: string; scope?: string; group_id?: number; model?: string; notification_owner: string }[]
  legacy_handoffs: { legacy_rule_id: number; platform?: string; group_id?: number; monitor_rules: string[] }[]
}
export type ChannelKind = 'email' | 'feishu_webhook' | 'feishu_app' | 'lark_webhook' | 'lark_app' | 'telegram' | 'wecom' | 'dingtalk' | 'qq' | 'webhook'
export interface NotificationChannel { id: string; kind: ChannelKind; name: string; enabled: boolean; settings: Record<string, string>; secret_configured: string[]; revision: number; updated_at?: string; last_state?: string; last_error?: string; last_accepted?: string }
export interface NotificationTarget { id: string; channel_id: string; name: string; address: string; enabled: boolean; settings: Record<string, string>; channel_kind?: ChannelKind }
export interface NotificationChannelInput extends NotificationChannel { secrets: Record<string, string>; clear_secrets: string[]; targets: NotificationTarget[] }
export interface ChannelList { channels: NotificationChannel[]; targets: NotificationTarget[]; kinds: ChannelKind[] }
export interface IncidentQuery { platform?: string; group_id?: number; model?: string; account_id?: number; time_range?: string; start_time?: string; end_time?: string; status?: string; severity?: string; rule?: string; before_time?: number; before_id?: string; limit?: number }
export interface IncidentPage { items: MonitorIncident[]; has_more: boolean; before_time: number; before_id: string }
export interface MonitorDetail { incident: MonitorIncident; history: MonitorHistory[]; history_has_more: boolean; history_before_id: number; outcomes: MonitorOutcomes; deliveries: MonitorDelivery[]; notification_constraint?: string }
export interface MonitorHealth { complete: boolean; health: { ready: boolean; loops: Record<string, { last_success: string; last_attempt: string; failed: boolean }> }; sources: { source_id: string; quality: string; last_success: string }[]; queue: { pending: number; uncertain: number; needs_attention: number; oldest_pending_ms?: number } }

export const opsMonitorAPI = {
  incidents: async (params: IncidentQuery, signal?: AbortSignal) => (await apiClient.get<IncidentPage>('/admin/ops/incidents', { params, signal })).data,
  detail: async (id: string, signal?: AbortSignal) => (await apiClient.get<MonitorDetail>(`/admin/ops/incidents/${encodeURIComponent(id)}`, { signal })).data,
  requests: async (id: string, after_id = '', signal?: AbortSignal) => (await apiClient.get<MonitorRecordPage<MonitorRelatedRequest>>(`/admin/ops/incidents/${encodeURIComponent(id)}/requests`, { params: { after_id }, signal })).data,
  accounts: async (id: string, after_id = '', signal?: AbortSignal) => (await apiClient.get<MonitorRecordPage<MonitorRelatedAccount>>(`/admin/ops/incidents/${encodeURIComponent(id)}/accounts`, { params: { after_id }, signal })).data,
  history: async (id: string, before_id = 0, kind = 'operations', signal?: AbortSignal) => (await apiClient.get<MonitorHistoryPage>(`/admin/ops/incidents/${encodeURIComponent(id)}/history`, { params: { before_id, kind }, signal })).data,
  control: async (item: MonitorIncident, action: string, reason = '', until?: string) => (await apiClient.post(`/admin/ops/incidents/${encodeURIComponent(item.id)}/control`, { action, reason, until, revision: item.revision, request_id: crypto.randomUUID() })).data,
  config: async () => (await apiClient.get<{ config: MonitorConfig; rules: MonitorRule[] }>('/admin/ops/monitor/config')).data,
  saveConfig: async (config: MonitorConfig) => (await apiClient.put<{ config: MonitorConfig; rules: MonitorRule[] }>('/admin/ops/monitor/config', config)).data,
  health: async (signal?: AbortSignal) => (await apiClient.get<MonitorHealth>('/admin/ops/monitor/health', { signal })).data,
  channels: async () => (await apiClient.get<ChannelList>('/admin/ops/notifications/channels')).data,
  saveChannel: async (input: NotificationChannelInput) => (await apiClient.put<ChannelList>(`/admin/ops/notifications/channels/${encodeURIComponent(input.id)}`, input)).data,
  testChannel: async (channel: string, target: string) => (await apiClient.post<{ state: string; error_class?: string; message?: string }>(`/admin/ops/notifications/channels/${encodeURIComponent(channel)}/test`, { target_id: target })).data,
  deliveries: async (params: { incident_id?: string; channel?: string; state?: string } = {}) => (await apiClient.get<MonitorDelivery[]>('/admin/ops/notifications/deliveries', { params })).data
}
