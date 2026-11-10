<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { getOutputRate } from '@/utils/outputRate'
import {
  LATENCY_BAR_CLASSES, LATENCY_BAR_FROM_CLASSES, LATENCY_BAR_TO_CLASSES,
  LATENCY_TEXT_CLASSES, durationSeverity, firstTokenSeverity
} from '@/utils/latencyHealth'

const props = defineProps<{
  row: {
    output_tokens: number
    duration_ms?: number | null
    first_token_ms?: number | null
    client_first_token_ms?: number | null
    gateway_wait_ms?: number | null
    pre_forward_ms?: number | null
    account_switch_count?: number | null
  }
}>()
const { t } = useI18n()
const appStore = useAppStore()
const mode = computed(() => appStore.cachedPublicSettings?.usage_output_rate_mode === 'total' ? 'total' : 'generation')
const rate = computed(() => getOutputRate(props.row, mode.value))
const rateHint = computed(() => t(rate.value == null ? 'usage.outputRateUnavailable' : mode.value === 'generation' ? 'usage.outputRateGenerationHint' : 'usage.outputRateTotalHint'))
const barClass = computed(() => firstToken.value != null
  ? ['bg-gradient-to-b from-40% to-60%', LATENCY_BAR_FROM_CLASSES[firstTokenSeverity(firstToken.value)], LATENCY_BAR_TO_CLASSES[durationSeverity(props.row.duration_ms ?? 0)]]
  : LATENCY_BAR_CLASSES[durationSeverity(props.row.duration_ms ?? 0)])

// 首字展示用户实际等待（含网关排队与换号）；旧记录没有该字段时回退到上游首字。
const firstToken = computed(() => props.row.client_first_token_ms ?? props.row.first_token_ms ?? null)
const QUEUE_HIGHLIGHT_MS = 1000
const showQueue = computed(() => (props.row.gateway_wait_ms ?? 0) >= QUEUE_HIGHLIGHT_MS)
const breakdownHint = computed(() => props.row.client_first_token_ms == null
  ? undefined
  : t('usage.latencyBreakdownHint', {
      client: formatDuration(props.row.client_first_token_ms),
      preForward: formatDuration(props.row.pre_forward_ms),
      wait: formatDuration(props.row.gateway_wait_ms ?? 0),
      switches: props.row.account_switch_count ?? 0,
      upstream: formatDuration(props.row.first_token_ms)
    }))

function formatDuration(ms: number | null | undefined): string {
  if (ms == null) return '-'
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(2)}s`
  const seconds = Math.round(ms / 1000)
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
}
</script>

<template>
  <div class="flex items-stretch gap-2 whitespace-nowrap">
    <span class="w-1 shrink-0 rounded-full" :class="barClass" aria-hidden="true"></span>
    <div class="grid grid-cols-[max-content_max-content] items-baseline gap-x-2 gap-y-0.5 text-xs">
      <span class="text-gray-400 dark:text-gray-500" :title="breakdownHint">{{ t('usage.latencyFirstToken') }}</span>
      <span v-if="firstToken != null" class="font-medium tabular-nums" :class="LATENCY_TEXT_CLASSES[firstTokenSeverity(firstToken)]" :title="breakdownHint" data-testid="usage-first-token">{{ formatDuration(firstToken) }}</span>
      <span v-else class="text-gray-400 dark:text-gray-500">-</span>
      <template v-if="showQueue">
        <span class="text-gray-400 dark:text-gray-500">{{ t('usage.latencyQueue') }}</span>
        <span class="font-medium tabular-nums text-amber-600 dark:text-amber-400" data-testid="usage-gateway-wait">{{ formatDuration(row.gateway_wait_ms) }}</span>
      </template>
      <span class="text-gray-400 dark:text-gray-500">{{ t('usage.latencyDuration') }}</span>
      <span class="font-medium tabular-nums" :class="LATENCY_TEXT_CLASSES[durationSeverity(row.duration_ms ?? 0)]">{{ formatDuration(row.duration_ms) }}</span>
      <span class="text-gray-400 dark:text-gray-500" :title="rateHint">tok/s</span>
      <span class="font-medium tabular-nums text-gray-700 dark:text-gray-200" :title="rateHint" data-testid="usage-output-rate">{{ rate == null ? '—' : rate.toFixed(1) }}</span>
    </div>
  </div>
</template>
