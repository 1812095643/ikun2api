import { apiClient } from '../client'
export interface MonitorIdentity { name?: string; email?: string; owner?: number }
export type MonitorNames = Record<string, MonitorIdentity>
export interface MonitorSample { request_id?: string; user_id?: number; key_id?: number; account_id?: number; group_id?: number }
export interface MonitorMember { rule: string; model?: string; count: number; last: string }
export interface MonitorLiveBucket { at: string; requests: number; succeeded: number; failed: number; cancelled: number; upstream_errors: number }
export interface MonitorLiveRequest { request: string; model: string; user_id: number; key_id: number; account_id: number; group_id: number; elapsed_ms: number; waiting_first_output: boolean }
export interface MonitorLiveAccount { account_id: number; requests: number; failed: number; upstream_errors: number; statuses: Record<string, number> }
export interface MonitorLive { from: string; to: string; buckets: MonitorLiveBucket[]; totals: { requests: number; succeeded: number; failed: number; cancelled: number; upstream_errors: number }; inflight_count: number; inflight: MonitorLiveRequest[]; problem_accounts: MonitorLiveAccount[]; first_output_p50_ms: number; first_output_p95_ms: number; names: MonitorNames; checked_at: string }
export interface Explanation { conclusion: string; confidence: string; facts: string[]; inferences: string[]; missing_evidence: string[]; advice: string[] }
export interface MonitorIncident { id: string; parent_id?: string; fingerprint: string; rule_id: string; rule_version: number; scope: string; revision: number; status: string; severity: string; first_seen: string; last_seen: string; count: number; request_count: number; user_count: number; account_count: number; acknowledged: boolean; silenced_until: string; quality: string; explanation: Explanation; group_id?: number; model?: string; sample?: MonitorSample; last_fault?: string; rollup?: boolean; members?: MonitorMember[]; digest_only?: boolean; closed_by?: string }
export interface MonitorDelivery { id: string; incident_id: string; revision: number; channel: string; target: string; kind: string; state: string; attempts: number; error_class?: string; accepted_at: string; next_attempt: string }
export interface MonitorProfile { queue_warn_seconds: number; queue_fail_seconds: number; first_warn_seconds: number; first_fail_seconds: number; stall_warn_seconds: number; stall_fail_seconds: number; task_sla_seconds: number }
export interface MonitorRuleConfig { enabled: boolean; notification_owner: 'legacy' | 'monitor'; version: number }
export interface MonitorOwnerOverride { rule_id: string; scope?: string; group_id?: number; model?: string; notification_owner: 'legacy' | 'monitor' }
export interface MonitorLegacyHandoff { legacy_rule_id: number; group_id?: number; platform?: string; monitor_rules: string[] }
export interface MonitorConfig { owner_overrides: MonitorOwnerOverride[]; legacy_handoffs: MonitorLegacyHandoff[]; version: number; actor_id: string; idempotency_key: string; shadow: boolean; site_id: string; rules: Record<string, MonitorRuleConfig>; profiles: Record<string, MonitorProfile>; balance_low_usd: number | null; subscription_low_ratio: number | null; feishu_targets: string[]; email_targets: string[]; detail_base_url: string; payment_grace_seconds: number; billing_grace_seconds: number }
export interface MonitorRule { id: string; title: string; advice: string; requires: string[]; recovery_samples: number; recovery_seconds: number; quiet_close?: boolean; quiet_close_seconds?: number }
export interface MonitorSummary { build: string; config: MonitorConfig; incidents: MonitorIncident[]; deliveries: MonitorDelivery[]; sources: { source_id: string; last_success: string; quality: string; dropped: number }[]; capabilities: { name: string; quality: string; reason?: string }[]; rules: MonitorRule[]; coverage_gaps: string[]; transports: Record<string, boolean>; checked_at: string; list_limit: number; names?: MonitorNames }
export interface MonitorDetail { incident: MonitorIncident; history: MonitorIncident[]; children: MonitorIncident[]; impact?: { users: number[]; accounts: number[] }; hits?: { at: string; healthy: boolean }[]; names?: MonitorNames }
export interface MonitorFeishuChannel { domain: 'feishu' | 'lark'; app_id: string; app_secret?: string }
export interface MonitorEmailChannel { host: string; port: number; security: 'ssl' | 'starttls'; username: string; password?: string; from: string }
export interface MonitorChannels { source: 'page' | 'env' | 'none'; feishu: MonitorFeishuChannel; feishu_secret_set: boolean; email: MonitorEmailChannel; email_password_set: boolean; updated_by?: string; updated_at: string }
export interface MonitorChannelsInput { feishu: MonitorFeishuChannel; email: MonitorEmailChannel }
export interface MonitorFeishuChats { bot_name: string; chats: { chat_id: string; name: string }[] }
const prefix = '/admin/ops/monitor'
export const monitorAPI = {
  async summary(signal?: AbortSignal): Promise<MonitorSummary> { return (await apiClient.get(`${prefix}/summary`, { signal })).data },
  async live(signal?: AbortSignal): Promise<MonitorLive> { return (await apiClient.get(`${prefix}/live`, { signal })).data },
  async detail(id: string): Promise<MonitorDetail> { return (await apiClient.get(`${prefix}/incidents/${encodeURIComponent(id)}`)).data },
  async save(config: MonitorConfig): Promise<MonitorConfig> { return (await apiClient.put(`${prefix}/config`, config)).data },
  async channels(): Promise<MonitorChannels> { return (await apiClient.get(`${prefix}/channels`)).data },
  async saveChannels(input: MonitorChannelsInput): Promise<MonitorChannels> { return (await apiClient.put(`${prefix}/channels`, input)).data },
  async feishuChats(): Promise<MonitorFeishuChats> { return (await apiClient.post(`${prefix}/channels/feishu/chats`, {})).data },
  async testChannel(channel: 'feishu' | 'email', target: string): Promise<{ sent: boolean }> { return (await apiClient.post(`${prefix}/channels/test`, { channel, target })).data },
  async control(incident: MonitorIncident, action: string, reason = '', until?: string): Promise<MonitorDetail> { return (await apiClient.post(`${prefix}/incidents/${encodeURIComponent(incident.id)}/control`, { action, reason, until: until || '0001-01-01T00:00:00Z', revision: incident.revision, idempotency_key: crypto.randomUUID() })).data },
}
export function monitorError(error: unknown): string { const e = error as { response?: { data?: { message?: string } }; message?: string }; return e.response?.data?.message || e.message || '请求失败，请刷新后重试' }
export function monitorTime(value: string): string { if (!value || value.startsWith('0001')) return '尚无记录'; return new Date(value).toLocaleString('zh-CN', { hour12: false }) }
export const monitorStatus: Record<string, string> = { candidate: '待确认', firing: '异常持续', recovering: '恢复观察中', resolved: '已恢复 / 已关闭', superseded: '规则版本已替换' }
export const deliveryStatus: Record<string, string> = { pending: '等待发送', leased: '发送中', accepted: '平台已接收', shadow: '影子记录 / 未发送', dead: '投递失败，待处理', superseded: '已合并到新版', suppressed: '静默抑制' }
