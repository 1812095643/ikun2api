import { apiClient } from './client'

export interface VaultSecret {
 email: string; password: string; auxiliary_email: string; auxiliary_password: string
 session_key: string; api_key: string; mail_url: string; auxiliary_url: string; notes: string
}
export interface VaultEntry extends VaultSecret {
 platform: string; title: string; tags: string; status: string; account_id: number | null
 existing_id?: string; expected_version?: number; action?: 'insert' | 'skip' | 'fill' | 'replace'
}
export interface VaultRecord {
 id: string; platform: string; title: string; tags: string; status: string
 email: string; auxiliary_email: string; account_id: number | null; fields: Record<string, boolean>
 version: number; updated_at: string
}
export interface VaultParseRow { line: number; entry: VaultEntry; error?: string }
export const vaultFields = [
 ['email', '邮箱'], ['password', '邮箱密码'], ['auxiliary_email', '辅助邮箱'],
 ['auxiliary_password', '辅助密码'], ['session_key', 'SessionKey'], ['api_key', 'API Key'],
 ['mail_url', '邮箱网址'], ['auxiliary_url', '辅邮网址'], ['notes', '备注']
] as const
export type VaultField = typeof vaultFields[number][0]
export function emptyVaultEntry(): VaultEntry {
 return { platform: 'claude', title: '', tags: '', status: 'unused', account_id: null,
  email: '', password: '', auxiliary_email: '', auxiliary_password: '', session_key: '', api_key: '', mail_url: '', auxiliary_url: '', notes: '' }
}
const base = '/admin/workspace/account-vault'
export const accountVaultAPI = {
 list: async (params: { page: number; page_size: number; query: string; platform: string; status: string }) => (await apiClient.get<{ items: VaultRecord[]; total: number }>(base, { params })).data,
 preview: async (raw: string) => (await apiClient.post<{ rows: VaultParseRow[] }>(base + '/preview', { raw })).data,
 import: async (batch_id: string, items: VaultEntry[]) => (await apiClient.post<Record<string, number>>(base + '/import', { batch_id, items })).data,
 reveal: async (id: string, field: string) => (await apiClient.post<{ value?: string; secret?: VaultSecret; version?: number }>(base + '/' + id + '/reveal', { field })).data,
 update: async (id: string, entry: VaultEntry) => (await apiClient.put(base + '/' + id, entry)).data,
 delete: async (ids: string[]) => (await apiClient.post<{ deleted: number }>(base + '/delete', { ids, confirm: true })).data,
}
