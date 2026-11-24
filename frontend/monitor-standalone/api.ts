import type { MonitorLive, MonitorChannels, MonitorChannelsInput, MonitorConfig, MonitorDetail, MonitorFeishuChats, MonitorIncident, MonitorSummary } from '../src/api/admin/monitor'
export type { MonitorLive, MonitorNames, MonitorChannels, MonitorChannelsInput, MonitorConfig, MonitorDetail, MonitorFeishuChats, MonitorIncident, MonitorSummary, MonitorProfile, MonitorRule } from '../src/api/admin/monitor'

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const token = localStorage.getItem('auth_token')
  if (!token) throw new Error('请先登录心链词元管理员后台，再打开异常监控')
  const response = await fetch(`/api/v1/admin/ops/monitor${path}`, {
    ...options,
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}`, ...options.headers },
  })
  const body = await response.json().catch(() => ({})) as { code?: number; data?: T; message?: string }
  if (!response.ok || body.code !== 0) {
    throw new Error(response.status === 401 || response.status === 403 ? '管理员会话已失效或无权访问，请重新登录' : body.message || '监控请求失败，请稍后重试')
  }
  return body.data as T
}
export const monitorAPI = {
  summary: (signal?: AbortSignal) => request<MonitorSummary>('/summary', { signal }),
  live: (signal?: AbortSignal) => request<MonitorLive>('/live', { signal }),
  detail: (id: string) => request<MonitorDetail>(`/incidents/${encodeURIComponent(id)}`),
  save: (config: MonitorConfig) => request<MonitorConfig>('/config', { method: 'PUT', body: JSON.stringify(config) }),
  channels: () => request<MonitorChannels>('/channels'),
  saveChannels: (input: MonitorChannelsInput) => request<MonitorChannels>('/channels', { method: 'PUT', body: JSON.stringify(input) }),
  feishuChats: () => request<MonitorFeishuChats>('/channels/feishu/chats', { method: 'POST', body: '{}' }),
  testChannel: (channel: 'feishu' | 'email', target: string) => request<{ sent: boolean }>('/channels/test', { method: 'POST', body: JSON.stringify({ channel, target }) }),
  control: (incident: MonitorIncident, action: string, reason = '', until?: string) => request<MonitorDetail>(`/incidents/${encodeURIComponent(incident.id)}/control`, { method: 'POST', body: JSON.stringify({ action, reason, until: until || '0001-01-01T00:00:00Z', revision: incident.revision, idempotency_key: crypto.randomUUID() }) }),
}
export function monitorError(error: unknown): string { return error instanceof Error ? error.message : '请求失败，请刷新后重试' }
export function monitorTime(value: string): string { if (!value || value.startsWith('0001')) return '尚无记录'; return new Date(value).toLocaleString('zh-CN', { hour12: false }) }
export const monitorStatus: Record<string, string> = { candidate: '待确认', firing: '异常持续', recovering: '恢复观察中', resolved: '已恢复 / 已关闭', superseded: '规则版本已替换' }
export const deliveryStatus: Record<string, string> = { pending: '等待发送', leased: '发送中', accepted: '平台已接收', shadow: '影子记录 / 未发送', dead: '投递失败，待处理', superseded: '已合并到新版', suppressed: '静默抑制' }
