import { apiClient } from '../client'
export interface BusinessPage<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}
export interface ReceivableAccount {
  user_id: number
  email: string
  username: string
  deleted: boolean
  is_downstream: boolean
  balance: string
  credited: string
  received: string
  outstanding: string
  last_credit_at?: string | null
  last_payment_at?: string | null
}
export interface ReceivableEntry {
  id: number
  user_id: number
  kind: 'credit' | 'payment'
  amount: string
  outstanding_after: string
  actor_id: number
  actor_name: string
  notes: string
  payment_method: string
  occurred_at: string
  created_at: string
}
export interface ReceivablePage extends BusinessPage<ReceivableAccount> {
  summary: {
    outstanding: string
    credited: string
    received: string
    debtors: number
  }
}
export interface ReceivableHistory extends BusinessPage<ReceivableEntry> {
  account: ReceivableAccount
}
export interface DownstreamGroupRate {
  group_id: number
  group_name: string
  rate: number
}
export interface DownstreamUser {
  group_rates: DownstreamGroupRate[]
  id: number
  email: string
  username: string
  status: string
  balance: string
  outstanding: string
  requests: number
  tokens: number
  cost: string
  last_used_at?: string | null
}
export interface DownstreamPage extends BusinessPage<DownstreamUser> {
  summary: { balance: string }
  from: string
  to: string
}
export interface DownstreamApply {
  user_ids: number[]
  is_downstream?: boolean
  group_rates?: Record<number, number | null>
  operation_key: string
}
export interface DownstreamUsage {
  group_id: number
  group_name: string
  model: string
  requests: number
  tokens: number
  cost: string
}
const base = '/admin/workspace'
export const businessAPI = {
  async receivables(params: Record<string, unknown>, signal?: AbortSignal) {
    return (
      await apiClient.get<ReceivablePage>(`${base}/receivables`, {
        params,
        signal
      })
    ).data
  },
  async history(id: number, page = 1, signal?: AbortSignal) {
    return (
      await apiClient.get<ReceivableHistory>(`${base}/receivables/${id}`, {
        params: { page },
        signal
      })
    ).data
  },
  async payment(
    id: number,
    request: {
      amount: string
      payment_method: string
      notes: string
      occurred_at?: string
      operation_key: string
    }
  ) {
    return (
      await apiClient.post<ReceivableEntry>(
        `${base}/receivables/${id}/payments`,
        request
      )
    ).data
  },
  async downstreams(params: Record<string, unknown>, signal?: AbortSignal) {
    return (
      await apiClient.get<DownstreamPage>(`${base}/downstreams`, {
        params,
        signal
      })
    ).data
  },
  async apply(request: DownstreamApply) {
    return (
      await apiClient.post<{ affected: number }>(
        `${base}/downstreams/apply`,
        request
      )
    ).data
  },
  async usage(
    id: number,
    params: Record<string, unknown>,
    signal?: AbortSignal
  ) {
    return (
      await apiClient.get<{ items: DownstreamUsage[] }>(
        `${base}/downstreams/${id}/usage`,
        { params, signal }
      )
    ).data
  }
}
