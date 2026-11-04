/**
 * User Subscription API
 * API for regular users to view their own subscriptions and progress
 */

import { apiClient } from './client'
import { universalSubscriptionsAPI, type UniversalSubscription } from './universalSubscriptions'
import type { Group } from '@/types'
import type { UserSubscription, SubscriptionProgress } from '@/types'

/**
 * Subscription summary for user dashboard
 */
export interface SubscriptionSummary {
  active_count: number
  subscriptions: Array<{
    id: number
    group_name: string
    status: string
    daily_progress: number | null
    weekly_progress: number | null
    monthly_progress: number | null
    expires_at: string | null
    days_remaining: number | null
  }>
}

/**
 * Get list of current user's subscriptions
 */
export async function getMySubscriptions(): Promise<UserSubscription[]> {
  const response = await apiClient.get<UserSubscription[]>('/subscriptions')
  return response.data
}

/**
 * Get current user's active subscriptions
 */
export async function getActiveSubscriptions(): Promise<UserSubscription[]> {
 const data = await universalSubscriptionsAPI.list()
 return data.items.filter(x => x.status === 'active').map(universalProgressAdapter)
}
function universalProgressAdapter(x: UniversalSubscription): UserSubscription {
 return { universal_remaining_usd: x.remaining_usd, universal_total_credit_usd: x.total_credit_usd, id: x.id, user_id: x.user_id, group_id: 0, status: 'active', starts_at: x.starts_at, expires_at: x.expires_at, daily_usage_usd: x.daily_used_usd, weekly_usage_usd: x.weekly_used_usd, monthly_usage_usd: x.monthly_used_usd,
  daily_window_start: x.daily_window_start || null, weekly_window_start: x.weekly_window_start || null, monthly_window_start: x.monthly_window_start || null, created_at: x.starts_at, updated_at: x.starts_at,
  group: { id: 0, name: x.name, platform: 'composite', subscription_type: 'subscription', rate_multiplier: 1, daily_limit_usd: x.daily_limit_usd, weekly_limit_usd: x.weekly_limit_usd, monthly_limit_usd: x.monthly_limit_usd } as Group }
}

/**
 * Get progress for all user's active subscriptions
 */
export async function getSubscriptionsProgress(): Promise<SubscriptionProgress[]> {
  const response = await apiClient.get<SubscriptionProgress[]>('/subscriptions/progress')
  return response.data
}

/**
 * Get subscription summary for dashboard display
 */
export async function getSubscriptionSummary(): Promise<SubscriptionSummary> {
  const response = await apiClient.get<SubscriptionSummary>('/subscriptions/summary')
  return response.data
}

/**
 * Get progress for a specific subscription
 */
export async function getSubscriptionProgress(
  subscriptionId: number
): Promise<SubscriptionProgress> {
  const response = await apiClient.get<SubscriptionProgress>(
    `/subscriptions/${subscriptionId}/progress`
  )
  return response.data
}

export default {
  getMySubscriptions,
  getActiveSubscriptions,
  getSubscriptionsProgress,
  getSubscriptionSummary,
  getSubscriptionProgress
}
