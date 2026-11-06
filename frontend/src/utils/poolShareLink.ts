import type { LocationQuery } from 'vue-router'
import type { UserPoolTab } from '@/composables/useUserPoolData'

export const POOL_SHARE_TOKEN_QUERY_KEY = 'share_token'

export function isPoolSharePagePath(path: string): boolean {
  return path === '/share/pool' || path === '/share/pool/'
}

function shareToken(value: unknown): string {
  return typeof value === 'string' && value.startsWith('pool_') ? value : ''
}

function fragmentToken(fragment: URLSearchParams, key: string): string {
  const values = fragment.getAll(key)
  return values.length === 1 ? shareToken(values[0]) : ''
}

/** The host site's login token is unrelated to the read-only share credential. */
export function resolvePoolShareToken(query: LocationQuery, hash: string): string {
  if (query[POOL_SHARE_TOKEN_QUERY_KEY] !== undefined) {
    return shareToken(query[POOL_SHARE_TOKEN_QUERY_KEY])
  }
  const fragment = new URLSearchParams(hash.replace(/^#/, ''))
  if (fragment.has(POOL_SHARE_TOKEN_QUERY_KEY)) {
    return fragmentToken(fragment, POOL_SHARE_TOKEN_QUERY_KEY)
  }
  if (query.token !== undefined) {
    const legacy = shareToken(query.token)
    if (legacy) return legacy
    // Old Sub2API embeds append their own JWT even to fragment-based links.
    if (query.ui_mode !== 'embedded' || typeof query.token !== 'string' || !query.token) return ''
  }
  return fragmentToken(fragment, 'token')
}

export function buildPoolShareUrl(origin: string, token: string, tab: UserPoolTab): string {
  const url = new URL('/share/pool', origin)
  url.searchParams.set(POOL_SHARE_TOKEN_QUERY_KEY, token)
  url.searchParams.set('tab', tab)
  return url.toString()
}

/** Upgrade a legacy link in place without rotating or issuing a credential. */
export function preservePoolShareCredential(url: URL): boolean {
  if (!isPoolSharePagePath(url.pathname)) return false
  if (!url.searchParams.has(POOL_SHARE_TOKEN_QUERY_KEY)) {
    const values = url.searchParams.getAll('token')
    const query: LocationQuery = {}
    if (values.length) query.token = values.length > 1 ? values : values[0]
    const token = resolvePoolShareToken(query, url.hash)
    if (token) url.searchParams.set(POOL_SHARE_TOKEN_QUERY_KEY, token)
  }
  url.searchParams.delete('token')
  return true
}
