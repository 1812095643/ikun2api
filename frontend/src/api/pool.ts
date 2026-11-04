/**
 * Account Pool API endpoints
 *
 * 「实时号池」是只读聚合视图：数据全部来自账号表与 usage_logs，
 * 不提供任何写入路径，因此这里的函数只有 GET。
 *
 * 两个作用域共用同一份数据结构：
 *   - admin: /admin/pool，全量字段；
 *   - user:  /pool，服务端强制钉上当前用户，返回脱敏字段。
 */

import { apiClient } from './client'
import type { AdminUsageLog, PaginatedResponse, UsageLog } from '@/types'

/** 号池视图作用域。 */
export type PoolScope = 'admin' | 'user'

/** 一个时间窗口内的用量聚合。 */
export interface PoolWindow {
  requests: number
  tokens: number
  cost: number
  standard_cost: number
  user_cost: number
}

/** 账号所属分组的展示引用。 */
export interface PoolGroupRef {
  id: number
  name: string
}

/** 号池中的账号视图（管理员口径）。 */
export interface PoolAccount {
  capacity?: PoolCapacity
  five_hour?: PoolQuotaWindow
  seven_day?: PoolQuotaWindow

  id: number
  name: string
  platform: string
  type: string
  status: string
  notes?: string

  schedulable: boolean
  error_message?: string
  last_used_at?: string
  rate_multiplier?: number
  priority?: number
  concurrency?: number
  load_factor?: number

  in_use?: number

  rate_limited_at?: string
  rate_limit_reset_at?: string
  overload_until?: string
  temp_unschedulable_until?: string
  temp_unschedulable_reason?: string

  session_window_start?: string
  session_window_end?: string
  session_window_status?: string

  parent_account_id?: number
  groups?: PoolGroupRef[]

  today: PoolWindow
}

/** 号池概览（管理员口径）。 */
export interface PoolOverview {
  total_accounts: number
  active_accounts: number
  error_accounts: number
  disabled_accounts: number
  schedulable_count: number
  platform_counts: Record<string, number>
  total_in_use: number
  today: PoolWindow
  window_7d: PoolWindow
  generated_at: string
}

/** 号池概览（用户口径，只有自己的数字）。 */
export interface PoolUserOverview {
  account_count: number
  active_count: number
  platform_counts: Record<string, number>
  today: PoolWindow
  window_7d: PoolWindow
  generated_at: string
}

export interface PoolAccountQuery {
  page?: number
  page_size?: number
  platform?: string
  type?: string
  status?: string
  search?: string
  group_id?: number
}

export interface PoolUsageQuery {
  page?: number
  page_size?: number
  start_time?: string
  end_time?: string
  user_id?: number
  account_id?: number
  api_key_id?: number
  model?: string
  request_id?: string
}

// ==================== API Functions ====================

const adminBase = '/admin/pool'
const userBase = '/pool'

function poolBase(scope: PoolScope): string {
  return scope === 'user' ? userBase : adminBase
}

/** 号池概览：账号状态分布 + 今日 / 近 7 天用量。 */
export async function getPoolOverview(
  scope: PoolScope = 'admin',
  options?: { signal?: AbortSignal }
): Promise<PoolOverview & PoolUserOverview> {
  return (await apiClient.get<PoolOverview & PoolUserOverview>(`${poolBase(scope)}/overview`, {
    signal: options?.signal
  })).data
}

/** 号池账号列表（含今日用量与当前并发占用）。 */
export async function listPoolAccounts(
  params: PoolAccountQuery,
  scope: PoolScope = 'admin',
  options?: { signal?: AbortSignal }
): Promise<PaginatedResponse<PoolAccount>> {
  return (await apiClient.get<PaginatedResponse<PoolAccount>>(`${poolBase(scope)}/accounts`, {
    params,
    signal: options?.signal
  })).data
}

/** 单个账号的近期请求流水。 */
export async function listPoolAccountRequests(
  accountId: number,
  params: PoolUsageQuery,
  scope: PoolScope = 'admin',
  options?: { signal?: AbortSignal }
): Promise<PaginatedResponse<AdminUsageLog>> {
  return (await apiClient.get<PaginatedResponse<AdminUsageLog>>(
    `${poolBase(scope)}/accounts/${accountId}/requests`,
    { params, signal: options?.signal }
  )).data
}

/** 号池口径的使用记录（与「使用记录」同源，附带账号快照）。 */
export async function listPoolUsage(
  params: PoolUsageQuery,
  scope: PoolScope = 'admin',
  options?: { signal?: AbortSignal }
): Promise<PaginatedResponse<AdminUsageLog & UsageLog>> {
  return (await apiClient.get<PaginatedResponse<AdminUsageLog & UsageLog>>(`${poolBase(scope)}/usage`, {
    params,
    signal: options?.signal
  })).data
}

export const poolAPI = {
  overview: getPoolOverview,
  accounts: listPoolAccounts,
  accountRequests: listPoolAccountRequests,
  usage: listPoolUsage
}

export default poolAPI

/** 号池单个用量窗口（5 小时 / 7 天）的读数。 */
export interface PoolQuotaWindow {
  requests?: number
  tokens?: number
  cost?: number
  utilization: number
  resets_at?: string
  remaining_seconds?: number
  source?: string
  enabled?: boolean
}

/** 号池容量视图：并发槽位 + 管理员配置的各类限额。 */
export interface PoolCapacity {
  in_use: number
  concurrency: number
  load_factor?: number
  window_cost_used?: number
  window_cost_limit?: number
  quota_used?: number
  quota_limit?: number
  quota_daily_used?: number
  quota_daily_limit?: number
  quota_weekly_used?: number
  quota_weekly_limit?: number
  supports_quota?: boolean
  supports_window_cost?: boolean
  quota_warning?: string
}
