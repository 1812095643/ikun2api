import { describe, it, expect } from 'vitest'
import type { MonitorCoverage, MonitorMatrixBucket, MonitorMetric } from '@/api/channelMonitorV2'
import { timelineSlots, hasMonitorSamples } from '../channelStatusCards'

const coverage = { bucket_seconds: 300, requested_end: '2026-11-08T03:15:00Z' } as MonitorCoverage
const bucket = (minute: string) => ({ bucket_start: `2026-11-08T03:${minute}:00Z` }) as MonitorMatrixBucket

describe('channel status data presentation', () => {
  it('keeps an interior data gap at its actual timestamp', () => {
    const slots = timelineSlots([bucket('00'), bucket('10')], coverage, 3)
    expect(slots.map(slot => slot.bucket?.bucket_start ?? null)).toEqual(['2026-11-08T03:00:00Z', null, '2026-11-08T03:10:00Z'])
  })
  it('does not treat redacted traffic counts as no data', () => {
    const metric = { request_count: 0, error_rate: 0, cache_rate: 0, ttft: { p50_ms: 2000 }, duration: { p50_ms: null } } as MonitorMetric
    expect(hasMonitorSamples(metric, { overall: 'unknown' } as never)).toBe(true)
  })
  it('does not present empty history as perfect availability', () => {
    const metric = { request_count: 0, error_rate: 0, cache_rate: 0, ttft: { p50_ms: null }, duration: { p50_ms: null } } as MonitorMetric
    expect(hasMonitorSamples(metric, { overall: 'unknown' } as never)).toBe(false)
  })
})
