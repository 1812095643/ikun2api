<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ConnectionTrace } from '@/api/admin/connectionDiagnostics'

const props = defineProps<{ row: ConnectionTrace }>()
const { t, te } = useI18n()
const ms = (value: number | null | undefined) => value == null ? '—' : value >= 1000 ? `${(value / 1000).toFixed(2)}s` : `${Math.round(value)}ms`
const kb = (value: number) => value >= 1048576 ? `${(value / 1048576).toFixed(1)} MB` : `${(value / 1024).toFixed(1)} KB`
const spanLabel = (name: string) => te(`connections.phases.${name}`) ? t(`connections.phases.${name}`) : t('connections.upstream')
const spans = computed(() => props.row.detail?.spans ?? [])
const width = (start: number, end: number) => ({ marginLeft: `${Math.max(0, Math.min(99, start / Math.max(1, props.row.total_ms) * 100))}%`, width: `${Math.max(0.5, Math.min(100, (end - start) / Math.max(1, props.row.total_ms) * 100))}%` })
function stages(events: Record<string, number>, end: number | undefined) {
  return [
    { label: 'connection', start: events.connection_start, end: events.connection_ready },
    { label: 'dns', start: events.dns_start, end: events.dns_end },
    { label: 'tcp', start: events.tcp_start, end: events.tcp_end },
    { label: 'tls', start: events.tls_start, end: events.tls_end },
    { label: 'request', start: events.connection_ready, end: events.request_written },
    { label: 'waiting', start: events.request_written, end: events.first_byte },
    { label: 'response', start: events.first_byte ?? events.response_headers, end }
  ].filter((stage): stage is { label: string; start: number; end: number } => stage.start != null && stage.end != null && stage.end >= stage.start)
}
</script>

<template>
  <div class="space-y-5">
    <dl class="grid grid-cols-2 gap-x-5 gap-y-3 border-b border-gray-200 pb-4 text-xs sm:grid-cols-4 dark:border-dark-700">
      <div v-for="item in [{ label: row.transport === 'websocket' ? 'connectionLifetime' : 'duration', value: row.total_ms }, { label: 'firstByte', value: row.first_byte_ms }, { label: 'firstToken', value: row.first_token_ms }, { label: 'routing', value: row.routing_ms }]" :key="item.label"><dt class="text-gray-500">{{ t(`connections.${item.label}`) }}</dt><dd class="mt-1 text-base font-semibold tabular-nums">{{ ms(item.value) }}</dd></div>
    </dl>
    <div class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 break-all text-xs"><span class="text-gray-500">{{ t('connections.request') }}</span><span class="font-mono">{{ row.request_id || '—' }}</span><span class="text-gray-500">{{ t('connections.trace') }}</span><span class="font-mono">{{ row.trace_id }}</span><span class="text-gray-500">{{ t('connections.bytes') }}</span><span class="tabular-nums">{{ t('connections.requestBytes') }} {{ kb(row.request_bytes) }} · {{ t('connections.responseBytes') }} {{ kb(row.response_bytes) }}</span></div>
    <p v-if="!row.detail_available" class="text-sm text-gray-500">{{ t('connections.summaryOnly') }}</p>
    <template v-if="row.detail">
      <div v-if="spans.length" class="space-y-2">
        <div v-for="(span, index) in spans" :key="index" class="grid grid-cols-[7rem_1fr_4rem] items-center gap-2 text-xs"><span class="truncate text-gray-600 dark:text-gray-400">{{ spanLabel(span.name) }}</span><div class="h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700"><div class="h-full rounded-full bg-primary-400" :style="width(span.start_ms, span.end_ms)" /></div><span class="text-right tabular-nums">{{ ms(span.end_ms - span.start_ms) }}</span></div>
      </div>
      <div v-for="attempt in row.detail.attempts" :key="attempt.number" class="space-y-2 border-t border-gray-100 pt-3 dark:border-dark-800">
        <div class="flex flex-wrap items-center justify-between gap-2 text-xs"><span class="font-semibold">{{ t('connections.attempt', { number: attempt.number }) }} · #{{ attempt.account_id }}</span><span class="text-gray-500">{{ attempt.reused === true ? t('connections.reused') : attempt.reused === false ? t('connections.newConnection') : '—' }} <span v-if="attempt.status">· HTTP {{ attempt.status }}</span></span></div>
        <div v-for="stage in stages(attempt.events, attempt.end_ms)" :key="stage.label" class="grid grid-cols-[7rem_1fr_4rem] items-center gap-2 text-xs"><span class="text-gray-500">{{ t(`connections.phases.${stage.label}`) }}</span><div class="h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700"><div class="h-full rounded-full" :class="stage.label === 'waiting' ? 'bg-amber-400' : 'bg-primary-500'" :style="width(stage.start, stage.end)" /></div><span class="text-right tabular-nums">{{ ms(stage.end - stage.start) }}</span></div>
      </div>
      <p v-if="row.detail.truncated" class="text-xs text-amber-600">{{ t('connections.truncated') }}</p>
    </template>
  </div>
</template>
