<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MonitorCoverage, MonitorMatrixBucket } from '@/api/channelMonitorV2'
import { formatMonitorMs, formatMonitorPercent } from '@/features/channel-monitor-v2/monitorFormat'
import { hasMonitorSamples, timelineSlots } from './channelStatusCards'

const props = defineProps<{ buckets: MonitorMatrixBucket[]; coverage: MonitorCoverage; length: number; countdown: number }>()
const { t, locale } = useI18n()
const active = shallowRef<number | null>(null)
const tooltipPosition = shallowRef({ left: 0, top: 0 })
function show(index: number, event: Event) {
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
  active.value = index
  tooltipPosition.value = { left: Math.max(136, Math.min(window.innerWidth - 136, rect.left + rect.width / 2)), top: rect.top - 8 }
}
const bars = computed(() => timelineSlots(props.buckets, props.coverage, props.length).map(slot => {
  const bucket = slot.bucket
  const present = !!bucket && hasMonitorSamples(bucket.metrics, bucket.health)
  const availability = present ? 1 - bucket!.metrics.error_rate : null
  const time = new Intl.DateTimeFormat(locale.value, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(slot.at)
  return {
    at: slot.at,
    style: !present ? 'bg-gray-200 dark:bg-dark-600' : availability! >= 0.98 ? 'bg-primary-500 dark:bg-primary-400' : availability! >= 0.90 ? 'bg-amber-400' : 'bg-red-400',
    height: !present ? '20%' : `${Math.max(25, Math.min(100, availability! * 100))}%`,
    label: present ? t('channelCards.timelineTooltip', { time, availability: formatMonitorPercent(availability!, locale.value), cache: formatMonitorPercent(bucket!.metrics.cache_rate, locale.value), ttft: formatMonitorMs(bucket!.metrics.ttft.p50_ms) }) : `${time} · ${t('channelCards.noSamples')}`,
  }
}))
</script>

<template>
  <div class="mt-auto border-t border-gray-100 pt-3 dark:border-dark-700">
    <div class="mb-2 flex items-center justify-between gap-2 text-[11px] text-gray-500 dark:text-gray-400">
      <span>{{ t('channelCards.recentRecords', { count: length }) }}</span>
      <span class="tabular-nums">{{ t('channelCards.refreshIn', { seconds: countdown }) }}</span>
    </div>
    <div class="flex h-6 gap-1" :aria-label="t('channelCards.history')">
      <button v-for="(bar, index) in bars" :key="bar.at" type="button" class="flex h-6 min-w-0 flex-1 items-end rounded-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary-500" :aria-label="bar.label" @mouseenter="show(index, $event)" @mouseleave="active = null" @focus="show(index, $event)" @blur="active = null" @click="show(index, $event)">
        <span class="block w-full rounded-sm transition-opacity hover:opacity-75 motion-reduce:transition-none" :class="bar.style" :style="{ height: bar.height }" />
      </button>
    </div>
    <div class="mt-1 flex justify-between text-[10px] text-gray-400"><span>{{ t('monitorCommon.past') }}</span><span>{{ t('monitorCommon.now') }}</span></div>
    <Teleport to="body">
      <div v-if="active != null && bars[active]" role="tooltip" class="pointer-events-none fixed z-[100] w-64 -translate-x-1/2 -translate-y-full rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-xs leading-relaxed text-gray-100 shadow-lg dark:border-dark-500 dark:bg-dark-700" :style="{ left: `${tooltipPosition.left}px`, top: `${tooltipPosition.top}px` }">{{ bars[active].label }}</div>
    </Teleport>
  </div>
</template>
