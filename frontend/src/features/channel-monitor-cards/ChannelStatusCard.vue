<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MonitorCoverage, MonitorMatrixRow } from '@/api/channelMonitorV2'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import { providerGradient, useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import { formatMonitorMs, formatMonitorPercent } from '@/features/channel-monitor-v2/monitorFormat'
import { hasMonitorSamples } from './channelStatusCards'
import ChannelStatusTimeline from './ChannelStatusTimeline.vue'

const props = defineProps<{ row: MonitorMatrixRow; coverage: MonitorCoverage; countdown: number; timelineLength: number; rate?: number }>()
const { t, locale } = useI18n()
const { providerLabel, providerBadgeClass } = useChannelMonitorFormat()
const present = computed(() => hasMonitorSamples(props.row.metrics, props.row.health))
const success = computed(() => present.value ? formatMonitorPercent(1 - props.row.metrics.error_rate, locale.value) : '—')
const cache = computed(() => present.value ? formatMonitorPercent(props.row.metrics.cache_rate, locale.value) : '—')
const successClass = computed(() => !present.value ? 'text-gray-400' : props.row.metrics.error_rate <= 0.02 ? 'text-primary-600 dark:text-primary-400' : props.row.metrics.error_rate <= 0.1 ? 'text-amber-600 dark:text-amber-400' : 'text-red-500 dark:text-red-400')
const rateLabel = computed(() => props.rate != null && Number.isFinite(props.rate) ? `${props.rate.toFixed(2)}x` : '—')
</script>

<template>
  <article class="card flex min-w-0 flex-col gap-5 p-5 transition-colors hover:border-primary-200 dark:hover:border-primary-700" data-testid="channel-status-card">
    <header class="flex min-w-0 items-start gap-3">
      <span class="grid h-10 w-10 shrink-0 place-items-center rounded-xl text-gray-700 ring-1 ring-black/5 dark:text-gray-200 dark:ring-white/10" :class="providerGradient(row.platform)"><ProviderIcon :provider="row.platform" :size="22" /></span>
      <div class="min-w-0 flex-1">
        <h3 class="truncate text-base font-semibold text-gray-900 dark:text-gray-100" :title="row.group_name">{{ row.group_name || t('channelCards.unknownGroup') }}</h3>
        <div class="mt-1 flex flex-wrap items-center gap-1.5 text-[10px] font-medium">
          <span class="rounded-md px-1.5 py-0.5" :class="providerBadgeClass(row.platform)">{{ providerLabel(row.platform) }}</span>
          <span class="rounded-md bg-gray-100 px-1.5 py-0.5 tabular-nums text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ t('channelCards.userRate') }} {{ rateLabel }}</span>
        </div>
      </div>
    </header>
    <dl class="grid grid-cols-3 gap-3 border-t border-gray-100 pt-5 dark:border-dark-700">
      <div class="min-w-0"><dt class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('channelCards.cacheRate') }}</dt><dd class="mt-1.5 text-lg font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ cache }}</dd></div>
      <div class="min-w-0"><dt class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('channelCards.successRate') }}</dt><dd class="mt-1.5 text-lg font-semibold tabular-nums" :class="successClass">{{ success }}</dd></div>
      <div class="min-w-0"><dt class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('channelCards.ttft') }}</dt><dd class="mt-1.5 text-lg font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ formatMonitorMs(row.metrics.ttft.p50_ms) }}</dd></div>
    </dl>
    <ChannelStatusTimeline :buckets="row.buckets" :coverage="coverage" :length="timelineLength" :countdown="countdown" />
  </article>
</template>
