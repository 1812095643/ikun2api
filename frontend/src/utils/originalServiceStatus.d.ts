export interface OfficialStatusWindow { start: number; end: number }
export interface OfficialStatusData {
  summary: Record<string, unknown>
  history: Record<string, unknown>
  window: OfficialStatusWindow
  fetchedAt: number | null
}
export function buildOfficialStack(data: OfficialStatusData): string
export function getOfficialWindow(): OfficialStatusWindow
