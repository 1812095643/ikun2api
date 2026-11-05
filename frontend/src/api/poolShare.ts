import { getAPIBaseURL } from './url'
import type { UserPoolAccount, UserPoolUsage } from './pool'
import type { PaginatedResponse } from '@/types'

export class PoolShareUnavailableError extends Error {
  constructor() {
    super('Pool share unavailable')
    this.name = 'PoolShareUnavailableError'
  }
}

// Do not use apiClient: a share must never inherit login credentials or refresh them.
async function listSharedPool<T>(resource: 'accounts' | 'usage', token: string, page: number, signal?: AbortSignal): Promise<PaginatedResponse<T>> {
  if (!token) throw new PoolShareUnavailableError()
  const params = new URLSearchParams({ page: String(page), page_size: '20' })
  const response = await fetch(`${getAPIBaseURL()}/pool-share/${resource}?${params}`, {
    signal,
    credentials: 'omit',
    referrerPolicy: 'no-referrer',
    cache: 'no-store',
    redirect: 'error',
    headers: { 'X-Pool-Share-Token': token }
  })
  if (response.status === 401 || response.status === 403) throw new PoolShareUnavailableError()
  if (!response.ok) throw new Error('Unable to load shared pool')
  const body = await response.json()
  if (body.code !== 0 || !Array.isArray(body.data?.items)) throw new Error('Invalid shared pool response')
  return body.data as PaginatedResponse<T>
}

export function listSharedPoolAccounts(token: string, page: number, signal?: AbortSignal) {
  return listSharedPool<UserPoolAccount>('accounts', token, page, signal)
}

export function listSharedPoolUsage(token: string, page: number, signal?: AbortSignal) {
  return listSharedPool<UserPoolUsage>('usage', token, page, signal)
}
