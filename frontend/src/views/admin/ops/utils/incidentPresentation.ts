import type { MonitorIncident } from '@/api/admin/opsMonitor'

export function incidentActive(item: MonitorIncident): boolean {
  return !['resolved', 'closed', 'superseded'].includes(item.status)
}

export function singleRequestWait(item: MonitorIncident): boolean {
  return ['L01', 'L02'].includes(item.rule_id) && !!item.sample?.request_id && !!item.scope?.endsWith(':' + item.sample.request_id)
}

export function incidentStatus(item: MonitorIncident): string {
  return item.status === 'resolved' && singleRequestWait(item) ? 'waitEnded' : item.status
}

export function incidentTime(value?: string | null): string {
  return value && Date.parse(value) > 0 ? new Date(value).toLocaleString() : '—'
}

export function incidentSilenced(item: MonitorIncident, at = Date.now()): boolean {
  return !!item.silenced_until && Date.parse(item.silenced_until) > at
}
