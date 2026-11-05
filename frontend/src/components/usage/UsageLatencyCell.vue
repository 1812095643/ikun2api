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
  row: { output_tokens: number; duration_ms?: number | null; first_token_ms?: number | null }
}>()
const { t } = useI18n()
const appStore = useAppStore()
const mode = computed(() => appStore.cachedPublicSettings?.usage_output_rate_mode === 'total' ? 'total' : 'generation')
const rate = computed(() => getOutputRate(props.row, mode.value))
const rateHint = computed(() => t(rate.value == null ? 'usage.outputRateUnavailable' : mode.value === 'generation' ? 'usage.outputRateGenerationHint' : 'usage.outputRateTotalHint'))
const barClass = computed(() => props.row.first_token_ms != null
  ? ['bg-gradient-to-b from-40% to-60%', LATENCY_BAR_FROM_CLASSES[firstTokenSeverity(props.row.first_token_ms)], LATENCY_BAR_TO_CLASSES[durationSeverity(props.row.duration_ms ?? 0)]]
  : LATENCY_BAR_CLASSES[durationSeverity(props.row.duration_ms ?? 0)])

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
      <span class="text-gray-400 dark:text-gray-500">{{ t('usage.latencyFirstToken') }}</span>
      <span v-if="row.first_token_ms != null" class="font-medium tabular-nums" :class="LATENCY_TEXT_CLASSES[firstTokenSeverity(row.first_token_ms)]">{{ formatDuration(row.first_token_ms) }}</span>
      <span v-else class="text-gray-400 dark:text-gray-500">-</span>
      <span class="text-gray-400 dark:text-gray-500">{{ t('usage.latencyDuration') }}</span>
      <span class="font-medium tabular-nums" :class="LATENCY_TEXT_CLASSES[durationSeverity(row.duration_ms ?? 0)]">{{ formatDuration(row.duration_ms) }}</span>
      <span class="text-gray-400 dark:text-gray-500" :title="rateHint">tok/s</span>
      <span class="font-medium tabular-nums text-gray-700 dark:text-gray-200" :title="rateHint" data-testid="usage-output-rate">{{ rate == null ? '—' : rate.toFixed(1) }}</span>
    </div>
  </div>
</template>
