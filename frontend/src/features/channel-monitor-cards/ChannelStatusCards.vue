<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import { formatMonitorPercent } from '@/features/channel-monitor-v2/monitorFormat'
import type { MonitorRange } from '@/api/channelMonitorV2'
import { hasMonitorSamples } from './channelStatusCards'
import { useChannelStatusCards } from './useChannelStatusCards'
import ChannelStatusCard from './ChannelStatusCard.vue'

const { t, locale } = useI18n()
const { providerLabel } = useChannelMonitorFormat()
const { range, snapshot, matrix, loading, error, rates, countdown, sections, timelineLength, reload, setRange } = useChannelStatusCards()
const ranges: MonitorRange[] = ['90m', '24h', '7d', '30d']
function time(value: string) { const date = new Date(value); return Number.isFinite(date.getTime()) ? new Intl.DateTimeFormat(locale.value, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(date) : '—' }
</script>

<template>
  <div class="space-y-6 pb-8">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <div><h1 class="page-title">{{ t('channelCards.title') }}</h1><p v-if="snapshot" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('channelCards.updatedTo', { time: time(snapshot.coverage.data_through) }) }}</p></div>
      <div class="flex items-center gap-2">
        <a href="?monitor_view=details" class="btn btn-ghost btn-sm">{{ t('channelCards.details') }}</a>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="reload"><Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" /><span>{{ t('common.refresh') }}</span></button>
      </div>
    </header>
    <div v-if="error" role="alert" class="flex items-center justify-between gap-3 rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300"><span>{{ error }}</span><button type="button" class="shrink-0 underline" :disabled="loading" @click="reload">{{ t('channelCards.retry') }}</button></div>
    <section class="card flex flex-wrap items-center gap-3 px-4 py-3" :aria-label="t('channelCards.timeRange')">
      <div class="tabs inline-flex" role="group" :aria-label="t('channelCards.timeRange')">
        <button v-for="option in ranges" :key="option" type="button" class="tab !px-3 !py-1.5 text-xs" :class="range === option ? 'tab-active' : ''" :aria-pressed="range === option" @click="setRange(option)">{{ option }}</button>
      </div>
      <span v-if="snapshot?.coverage.bootstrap?.active" class="text-xs text-gray-500 dark:text-gray-400">{{ t('channelCards.historyLoading') }}</span>
      <span v-if="snapshot && hasMonitorSamples(snapshot.metrics, snapshot.health)" class="ml-auto text-xs tabular-nums text-gray-500 dark:text-gray-400">{{ t('channelCards.summary', { success: formatMonitorPercent(1 - snapshot.metrics.error_rate, locale), cache: formatMonitorPercent(snapshot.metrics.cache_rate, locale) }) }}</span>
    </section>
    <div v-if="loading && !matrix" class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4" aria-busy="true"><div v-for="i in 6" :key="i" class="h-64 animate-pulse rounded-2xl bg-gray-100 dark:bg-dark-800" /></div>
    <EmptyState v-else-if="sections.length === 0 && !error" :title="t('channelCards.emptyTitle')" :description="t('channelCards.emptyDescription')" />
    <section v-for="section in sections" :key="section.platform" class="space-y-3" :aria-label="providerLabel(section.platform)">
      <h2 class="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">{{ providerLabel(section.platform) }}<span class="rounded-md bg-gray-100 px-1.5 py-0.5 text-[10px] tabular-nums dark:bg-dark-700">{{ section.rows.length }}</span></h2>
      <div class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4"><ChannelStatusCard v-for="row in section.rows" :key="`${row.platform}:${row.group_id}`" :row="row" :coverage="matrix!.coverage" :countdown="countdown" :timeline-length="timelineLength" :rate="rates[row.group_id!]" /></div>
    </section>
  </div>
</template>
