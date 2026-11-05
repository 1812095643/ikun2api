<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserPoolUsage } from '@/api/pool'
import type { Column } from '@/components/common/types'
import DataTable from '@/components/common/DataTable.vue'
import UsageTokenCounts from '@/components/usage/UsageTokenCounts.vue'
import UsageLatencyCell from '@/components/usage/UsageLatencyCell.vue'
import { formatDateTime, formatReasoningEffort } from '@/utils/format'

defineProps<{ rows: UserPoolUsage[]; loading: boolean; failed: boolean }>()
const { t } = useI18n()
const columns = computed<Column[]>(() => [
  { key: 'name', label: t('admin.pool.columns.account') },
  { key: 'model', label: t('admin.pool.usage.columns.model') },
  { key: 'reasoning_effort', label: t('usage.reasoningEffort') },
  { key: 'inbound_endpoint', label: t('admin.pool.usage.columns.endpoint') },
  { key: 'group_name', label: t('admin.pool.extra.group') },
  { key: 'stream', label: t('admin.pool.extra.type') },
  { key: 'tokens', label: t('usage.tokens') },
  { key: 'latency', label: t('usage.latency') },
  { key: 'created_at', label: t('admin.pool.usage.columns.time') }
])
</script>

<template>
  <DataTable :columns="columns" :data="rows" :loading="loading && !rows.length">
    <template #cell-name="{ value }"><span class="inline-block max-w-[220px] truncate align-middle font-medium text-gray-900 dark:text-white" :title="value">{{ value }}</span></template>
    <template #cell-model="{ value }"><span class="inline-block max-w-[220px] truncate align-middle" :title="value">{{ value }}</span></template>
    <template #cell-reasoning_effort="{ value }"><span class="whitespace-nowrap">{{ formatReasoningEffort(value) }}</span></template>
    <template #cell-inbound_endpoint="{ value }"><span class="inline-block max-w-[220px] truncate align-middle font-mono text-xs text-gray-500 dark:text-gray-400" :title="value || ''">{{ value || '—' }}</span></template>
    <template #cell-group_name="{ value }"><span v-if="value" class="badge badge-primary max-w-[180px] truncate" :title="value">{{ value }}</span><span v-else class="text-gray-400">—</span></template>
    <template #cell-stream="{ value }"><span class="whitespace-nowrap text-xs text-gray-500 dark:text-gray-400">{{ t(value ? 'admin.pool.extra.streaming' : 'admin.pool.extra.nonStreaming') }}</span></template>
    <template #cell-tokens="{ row }"><UsageTokenCounts :row="row" /></template>
    <template #cell-latency="{ row }"><UsageLatencyCell :row="row" /></template>
    <template #cell-created_at="{ value }"><span class="whitespace-nowrap text-gray-500 dark:text-gray-400">{{ formatDateTime(value) }}</span></template>
    <template #empty><p class="py-12 text-center text-sm text-gray-500">{{ failed ? t('admin.pool.networkError') : t('admin.pool.usage.empty') }}</p></template>
  </DataTable>
</template>
