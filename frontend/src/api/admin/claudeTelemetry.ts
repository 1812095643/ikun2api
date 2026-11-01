import { apiClient } from '../client'

export interface ClaudeTelemetrySettings {
  enabled: boolean
  flush_interval_seconds: number
  min_send_gap_seconds: number
  timeout_seconds: number
}

export async function getClaudeTelemetrySettings(): Promise<ClaudeTelemetrySettings> {
  return (await apiClient.get<ClaudeTelemetrySettings>('/admin/settings/claude-telemetry')).data
}

export async function updateClaudeTelemetrySettings(value: ClaudeTelemetrySettings): Promise<ClaudeTelemetrySettings> {
  return (await apiClient.put<ClaudeTelemetrySettings>('/admin/settings/claude-telemetry', value)).data
}
