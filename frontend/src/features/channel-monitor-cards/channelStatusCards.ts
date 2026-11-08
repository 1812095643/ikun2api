import type { MonitorCoverage, MonitorMatrixBucket, MonitorMetric, MonitorHealth } from '@/api/channelMonitorV2'

export function hasMonitorSamples(metrics: MonitorMetric, health: MonitorHealth): boolean {
  // User responses deliberately redact request/token counts. Never infer an
  // outage or a perfect success rate from those zeroed privacy fields.
  return health.overall !== 'unknown' || health.score != null ||
    metrics.ttft.p50_ms != null || metrics.duration.p50_ms != null || metrics.error_rate > 0 || metrics.cache_rate > 0
}

export function timelineSlots(buckets: MonitorMatrixBucket[], coverage: MonitorCoverage, count: number) {
  const step = Math.max(1, coverage.bucket_seconds) * 1000
  const latest = Math.max(...buckets.map(bucket => Date.parse(bucket.bucket_start)).filter(Number.isFinite), 0)
  const end = Date.parse(coverage.requested_end || '') || (latest ? latest + step : Math.ceil(Date.now() / step) * step)
  const start = end - count * step
  const indexed = new Map(buckets.map(bucket => [Math.floor(Date.parse(bucket.bucket_start) / step), bucket]))
  return Array.from({ length: count }, (_, index) => {
    const at = start + index * step
    return { at, bucket: indexed.get(Math.floor(at / step)) ?? null }
  })
}
