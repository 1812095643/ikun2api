import { apiClient } from './client'
import type { AxiosResponse } from 'axios'

export type TicketStatus = 'open' | 'in_progress' | 'waiting_user' | 'resolved' | 'closed'
export interface TicketImage {
  id: string
  name: string
  width: number
  height: number
  size: number
  deleted: boolean
}
export interface TicketMessage {
  id: number
  author_id: number
  author_role: string
  author_name: string
  kind: string
  content: string
  event_type: string
  created_at: string
  images: TicketImage[]
}
export interface Ticket {
  id: number
  user_id: number
  user_email: string
  user_name: string
  subject: string
  category: string
  priority: string
  status: TicketStatus
  origin: 'user' | 'admin'
  created_at: string
  updated_at: string
  ended_at: string | null
  unread_count: number
  queue_position: number
  messages?: TicketMessage[]
}
export interface TicketPage {
  items: Ticket[]
  total: number
  page: number
  page_size: number
  stats: { active: number; unread: number; ended: number }
  queue: { total: number }
  quota?: { limit: number; used: number; can_create: boolean; ticket_id: number; resets_at: string }
}
export interface TicketDraft {
  content: string
  images: { name: string; data: string }[]
  subject?: string
  category?: string
  priority?: string
}

export interface TicketNotificationDraft extends TicketDraft {
  user_id: number
  request_key: string
}

export function ticketAPI(admin: boolean) {
  const base = admin ? '/admin/workspace/tickets' : '/workspace/tickets'
  return {
    async list(params: { page: number; search: string; status: string }, signal?: AbortSignal) {
      return (await apiClient.get<TicketPage>(base, { params, signal })).data
    },
    async get(id: number, signal?: AbortSignal) {
      return (await apiClient.get<Ticket>(`${base}/${id}`, { signal })).data
    },
    async create(draft: TicketDraft) {
      return (await apiClient.post<{ id: number }>('/workspace/tickets', draft)).data
    },
    async notify(draft: TicketNotificationDraft) {
      return (await apiClient.post<{ id: number; already_sent: boolean }>('/admin/workspace/tickets/notify', draft)).data
    },
    async reply(id: number, draft: TicketDraft) {
      return (await apiClient.post(`${base}/${id}/messages`, draft)).data
    },
    async status(id: number, status: TicketStatus) {
      return (await apiClient.post(`${base}/${id}/status`, { status })).data
    },
    async read(id: number) {
      await apiClient.post(`${base}/${id}/read`, {})
    },
    async image(id: number, image: string, signal?: AbortSignal) {
      return (
        await apiClient.get<Blob>(`${base}/${id}/images/${image}`, { responseType: 'blob', signal })
      ).data
    },
    async deleteImage(id: number, image: string) {
      await apiClient.delete(`${base}/${id}/images/${image}`, { data: { confirm: true } })
    },
    async delete(id: number) {
      await apiClient.delete(`${base}/${id}`, { data: { confirm: true } })
    },
    async cleanup() {
      return (await apiClient.post<{ deleted: number }>(`${base}/cleanup`, { confirm: true })).data
    }
  }
}

export interface Taxonomy {
  id: number
  name: string
  iconId?: string
  type?: number
}
export interface NewsItem {
  id: number
  title: string
  summary: string
  category?: Taxonomy
  tags: Taxonomy[]
  source: Taxonomy
  publishedAt: number
  updatedAt: number
  favorited?: boolean
  latestId?: number
  xPost?: { text?: string; media?: unknown[] } | null
}
export interface FeedPage<T> {
  items: T[]
  nextCursor?: string | null
  hasMore?: boolean
}
export interface FeedState {
  stale: boolean
  fetchedAt: number | null
}
function feedData<T extends object>(response: AxiosResponse<T>): T & { feedState: FeedState } {
  const timestamp = Date.parse(String(response.headers['x-workspace-fetched-at'] || ''))
  return {
    ...response.data,
    feedState: {
      stale: response.headers['x-workspace-cache'] === 'stale',
      fetchedAt: Number.isFinite(timestamp) ? timestamp : null
    }
  }
}
export interface ResetEvent {
  id: number
  resetAt: string
  resetType: number
  resetCreditExpiresAt?: string | null
  label: string
  sourceUrl?: string | null
}
export interface ResetPost {
  id: number
  tweetId: string
  sourceUrl: string
  originalText: string
  translatedText: string
  category: number
  publishedAt: string
  isReply: boolean
  replyingTo?: { username: string; tweetId: string } | null
}
export interface ResetSummary {
  lastResetAt: string | null
  recentResets: ResetEvent[]
  updatedAt: string
  stats: { totalResets: number; averageResetGapDays: number; longestWaitDays: number }
  announcedWindow?: { startAt: string; endAt: string } | null
  announcedWindowSourcePost?: ResetPost | null
}
export const workspaceAPI = {
  async serviceStatus(signal?: AbortSignal) {
    return feedData(await apiClient.get<{ summary: Record<string, unknown> }>('/workspace/reset/service-status', { signal }))
  },
  async serviceHistory(start: number, end: number, signal?: AbortSignal) {
    return feedData(await apiClient.get<Record<string, unknown>>('/workspace/reset/service-history', {
      params: { start_at: new Date(start).toISOString(), end_at: new Date(end).toISOString() }, signal
    }))
  },
  async news(
    view: string,
    params: Record<string, string | number | undefined>,
    signal?: AbortSignal
  ) {
    return feedData(await apiClient.get<FeedPage<NewsItem>>(`/workspace/news/${view}`, { params, signal }))
  },
  async taxonomy(path: string, signal?: AbortSignal) {
    return feedData(
      await apiClient.get<FeedPage<Taxonomy>>(`/workspace/news/${path}`, {
        params: { limit: 50 },
        signal
      })
    )
  },
  async favorites() {
    return (await apiClient.get<FeedPage<NewsItem>>('/workspace/favorites')).data
  },
  async favorite(item: NewsItem) {
    await apiClient.put(`/workspace/favorites/${item.id}`, item)
  },
  async unfavorite(id: number) {
    await apiClient.delete(`/workspace/favorites/${id}`)
  },
  async resetSummary(signal?: AbortSignal) {
    return feedData(await apiClient.get<ResetSummary>('/workspace/reset/summary', { signal }))
  },
  async resetPosts(params: Record<string, string | number | undefined>, signal?: AbortSignal) {
    return feedData(
      await apiClient.get<FeedPage<ResetPost> & { counts: { category: number; count: number }[] }>(
        '/workspace/reset/posts',
        { params, signal }
      )
    )
  },
  async resetContext(id: number, kind: 'reply-context' | 'announcement-window') {
    return (await apiClient.get<Record<string, unknown>>(`/workspace/reset/posts/${id}/${kind}`))
      .data
  }
}

export function workspaceError(error: unknown): string {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string')
    return error.message
  return '暂时无法完成，请稍后重试。'
}
export function formatTime(value?: string | number | null): string {
  if (!value) return '暂无记录'
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? '暂无记录'
    : date.toLocaleString('zh-CN', {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        hour12: false
      })
}
export function safeExternalURL(value?: string | null): string | undefined {
  if (!value) return undefined
  try {
    const url = new URL(value)
    return ['https:', 'http:'].includes(url.protocol) && !url.username && !url.password
      ? url.href
      : undefined
  } catch {
    return undefined
  }
}
