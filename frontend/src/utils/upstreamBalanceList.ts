import type { UpstreamBalance, UpstreamBalanceDisplay } from '@/api/upstreamBalances'

export function orderUpstreamBalances(items: UpstreamBalance[], display: UpstreamBalanceDisplay) {
  const positions = new Map(display.order.map((id, index) => [id, index]))
  const base = [...items].sort((a, b) => (positions.get(a.id) ?? Infinity) - (positions.get(b.id) ?? Infinity) || a.url.localeCompare(b.url))
  if (display.sort_by === 'manual') return base
  return base.sort((a, b) => {
    if (display.sort_by === 'name') return a.name.localeCompare(b.name, 'zh-CN')
    if (display.sort_by === 'recent') return (Date.parse(b.last_checked_at || '') || 0) - (Date.parse(a.last_checked_at || '') || 0)
    const av = a.snapshot.balance, bv = b.snapshot.balance
    const aMissing = av == null || !Number.isFinite(av), bMissing = bv == null || !Number.isFinite(bv)
    if (aMissing || bMissing) return Number(aMissing) - Number(bMissing)
    const unit = (a.snapshot.unit || '').localeCompare(b.snapshot.unit || '')
    return unit || (display.sort_by === 'balance_asc' ? av! - bv! : bv! - av!)
  })
}

// Replace only the reordered subset. Hidden rows and other pages retain their slots.
export function mergeUpstreamOrder(all: string[], reordered: string[]) {
  const known = new Set(all)
  const next = [...new Set(reordered)].filter(id => known.has(id))
  const included = new Set(next)
  let cursor = 0
  return all.map(id => included.has(id) ? next[cursor++] : id)
}

export function upstreamHasError(row: UpstreamBalance) {
  return !!row.last_checked_at && (row.snapshot.successful < row.snapshot.total || !!row.snapshot.error)
}
