import { apiClient } from '../client'

export interface ConnectionSpan { name: string; start_ms: number; end_ms: number }
export interface ConnectionAttempt { number: number; account_id: number; start_ms: number; end_ms?: number; status: number; error?: string; reused: boolean | null; request_bytes: number; response_bytes: number; body_eof: boolean; events: Record<string, number> }
export interface ConnectionDetail { total_ms: number; truncated: boolean; body_read_ms: number; downstream_write_ms: number; body_complete: boolean; downstream_error: boolean; events: Record<string, number>; spans: ConnectionSpan[]; attempts: ConnectionAttempt[] }
export interface ConnectionTrace {
  trace_id: string; request_id: string; client_request_id: string; started_at: string; user_id: number; api_key_id: number; account_id: number; group_id: number; model: string; path: string; transport: string; state: string; status: number; total_ms: number; first_byte_ms: number | null; first_token_ms: number | null; auth_ms: number | null; routing_ms: number | null; upstream_ms: number | null; request_bytes: number; response_bytes: number; error_type: string; detail_available: boolean; detail?: ConnectionDetail
}
export interface ConnectionQuery { start_time: string; end_time: string; request_id?: string; model?: string; user_id?: number; account_id?: number; group_id?: number; state?: string; transport?: string; cursor?: string; limit?: number }
export interface ConnectionPage { items: ConnectionTrace[]; next_cursor: string; retention_days: number; health: { written: number; dropped: number; write_failed: number; queued: number } }

export const listConnectionTraces = async (params: ConnectionQuery, signal?: AbortSignal) => (await apiClient.get<ConnectionPage>('/admin/ops/connections', { params, signal })).data
export const getConnectionTrace = async (row: ConnectionTrace, signal?: AbortSignal) => (await apiClient.get<ConnectionTrace>(`/admin/ops/connections/${row.trace_id}`, { params: { started_at: row.started_at }, signal })).data
