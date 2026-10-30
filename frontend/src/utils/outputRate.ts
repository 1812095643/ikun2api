export type OutputRateMode = 'total' | 'generation'

interface OutputTiming {
  output_tokens: number
  duration_ms?: number | null
  first_token_ms?: number | null
}

export function getOutputRate(row: OutputTiming, mode: OutputRateMode): number | null {
  const { output_tokens: tokens, duration_ms: duration, first_token_ms: firstToken } = row
  if (!Number.isFinite(tokens) || tokens < 0 || duration == null || !Number.isFinite(duration) || duration <= 0) return null
  if (mode === 'generation' && firstToken != null && (!Number.isFinite(firstToken) || firstToken < 0)) return null
  // 缺少首字记录或首字与总耗时相同，按总耗时回退；字段单位为毫秒。
  const useTotalDuration = mode === 'total' || firstToken == null || firstToken === duration
  const elapsed = useTotalDuration ? duration : duration - firstToken!
  if (elapsed <= 0) return null
  const rate = tokens / (elapsed / 1000)
  return Number.isFinite(rate) ? rate : null
}
