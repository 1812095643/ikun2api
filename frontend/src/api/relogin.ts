import { apiClient } from './client'

export interface ReloginAccount {
  id: number
  email: string
  name: string
  status: string
  schedulable: boolean
  plan_type: string
  error_message: string
  has_imported: boolean
  updated_at: string
}
export interface LoginSecret {
  email: string
  password?: string
  mfa_secret?: string
  mfa_code?: string
  inbox_url?: string
  auth_mode: 'password_2fa' | 'email_otp'
}
export interface ImportedAccount {
  email: string
  auth_mode: string
  has_password: boolean
  has_mfa: boolean
  has_inbox: boolean
  created_at: string
  updated_at: string
  last_used_at: string | null
}
export interface LoginRun {
  id: number
  email: string
  account_id: number | null
  trigger: string
  status: string
  started_at: string
  finished_at: string | null
  error_message: string
  plan_type: string
  applied: boolean
  apply_action: string
  challenge?: {
    id: string
    type: string
    message?: string
    masked_email?: string
    masked_phone?: string
    phone_channel?: string
  }
}
export interface ReloginSettings {
  auto_enabled: boolean
  template: {
    proxy_id: number | null
    group_ids: number[]
    concurrency: number
    priority: number
    rate_multiplier: number
  }
  quota_enabled: boolean
  quota_interval: number
  quota_threshold: number
  recipients: string[]
}
export interface ReloginState {
  settings: ReloginSettings
  state: { last_auto_run?: string; last_auto_count?: number }
  quota: {
    updated_at: string
    total_remaining_percent: number
    alerted: boolean
    items: { account_id: number; email: string; remaining_percent: number; reset_at: number }[]
  } | null
}
export interface ReloginOptions {
  groups: { id: number; name: string }[]
  proxies: { id: number; name: string }[]
}
export interface ImportedPage {
  items: ImportedAccount[]
  total: number
  page: number
  page_size: number
}
export interface InvalidImported {
  items: { email: string; updated_at: string }[]
  revision: string
}
const base = '/admin/workspace/relogin'
export const reloginAPI = {
  async exportRun(id: number, format: string) {
    return (await apiClient.post<{ format: string; content: unknown }>(`${base}/history/${id}/export`, { format })).data
  },
  async accounts() {
    return (await apiClient.get<{ items: ReloginAccount[] }>(`${base}/accounts`)).data.items
  },
  async imported(params: { page: number; query: string }) {
    return (await apiClient.get<ImportedPage>(`${base}/imported`, { params })).data
  },
  async history() {
    return (await apiClient.get<{ items: LoginRun[] }>(`${base}/history`)).data.items
  },
  async run(id: number) {
    return (await apiClient.get<LoginRun>(`${base}/history/${id}`)).data
  },
  async settings() {
    return (await apiClient.get<ReloginState>(`${base}/settings`)).data
  },
  async options() {
    return (await apiClient.get<ReloginOptions>(`${base}/options`)).data
  },
  async saveSettings(settings: ReloginSettings) {
    await apiClient.put(`${base}/settings`, settings)
  },
  async import(items: LoginSecret[]) {
    return (await apiClient.post<{ saved: number }>(`${base}/imported`, { items })).data
  },
  async export(emails: string[]) {
    return (await apiClient.post<{ items: LoginSecret[] }>(`${base}/imported/export`, { emails }))
      .data.items
  },
  async remove(emails: string[]) {
    await apiClient.post(`${base}/imported/delete`, { emails, confirm: true })
  },
  async invalid() {
    return (await apiClient.get<InvalidImported>(`${base}/imported/invalid`)).data
  },
  async removeInvalid(revision: string) {
    return (
      await apiClient.post<{ deleted: number }>(`${base}/imported/invalid`, {
        revision,
        confirm: true
      })
    ).data
  },
  async start(payload: LoginSecret & { account_id?: number; use_saved?: boolean; apply: boolean }) {
    return (await apiClient.post<{ id: number; status: string }>(`${base}/start`, payload)).data
  },
  async resume(
    id: number,
    payload: {
      email_code?: string
      phone_code?: string
      phone_number?: string
      phone_channel?: string
    }
  ) {
    return (await apiClient.post(`${base}/resume/${id}`, payload)).data
  },
  async apply(id: number) {
    await apiClient.post(`${base}/apply/${id}`, {}, { timeout: 180000 })
  },
  async refreshToken(refresh_token: string, account_id?: number) {
    return (
      await apiClient.post<LoginRun>(
        `${base}/refresh-token`,
        { refresh_token, account_id },
        { timeout: 180000 }
      )
    ).data
  },
  async auto() {
    return (await apiClient.post<{ queued: number }>(`${base}/auto`, {}, { timeout: 180000 })).data
  },
  async clearHistory() {
    await apiClient.post(`${base}/history/clear`, { confirm: true })
  }
}
export const runLabels: Record<string, string> = {
  apply_failed: '等待重试写回',
  running: '进行中',
  success: '验证成功',
  challenge: '等待验证',
  applied: '已写回',
  paused_free: '免费套餐已暂停',
  failed: '未完成',
  interrupted: '服务重启中断'
}
