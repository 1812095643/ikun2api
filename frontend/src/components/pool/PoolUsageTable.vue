<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AdminUsageLog, UsageLog } from '@/types'
import DataTable from '@/components/common/DataTable.vue'
import type { Column } from '@/components/common/types'
import Icon from '@/components/icons/Icon.vue'
import { formatCostFixed, formatNumber, formatTime } from '@/utils/format'
const props = defineProps<{ rows: (AdminUsageLog & UsageLog)[]; loading: boolean; failed: boolean; isAdmin: boolean }>()
const isAdmin = computed(() => props.isAdmin)
const { t } = useI18n()
const usageColumns = computed<Column[]>(() => {
  const base: Column[] = []
  if (isAdmin.value) {
    base.push({ key: 'user', label: t('admin.pool.usage.columns.user') })
  }
  base.push(
    { key: 'account', label: t('admin.pool.usage.columns.account') },
    { key: 'model', label: t('admin.pool.usage.columns.model') },
    { key: 'reasoning_effort', label: t('admin.pool.extra.reasoningEffort') },
    { key: 'endpoint', label: t('admin.pool.usage.columns.endpoint') },
    { key: 'group_name', label: t('admin.pool.extra.group') },
    { key: 'stream_type', label: t('admin.pool.extra.type') },
    { key: 'tokens', label: t('admin.pool.usage.columns.tokens'), class: 'text-right' },
    { key: 'cost', label: t('admin.pool.usage.columns.cost'), class: 'text-right' },
    { key: 'duration', label: t('admin.pool.usage.columns.duration'), class: 'text-right' },
    { key: 'created_at', label: t('admin.pool.usage.columns.time') },
    { key: 'user_agent', label: t('admin.pool.extra.userAgent') },
    { key: 'ip_address', label: t('admin.pool.extra.ipAddress') }
  )
  return base
})

/** 账号列优先用写入时固化的快照，改名或删除后历史仍可读。 */
function accountLabel(row: AdminUsageLog & UsageLog): string {
  const snapshot = row.account_name_snapshot
  if (snapshot) return snapshot
  if (row.account?.name) return row.account.name
  return row.account_id ? t('admin.pool.usage.accountDeleted') : '—'
}

/** 费用：管理员看账号侧统计成本，用户看自己实付。 */
function rowCost(row: AdminUsageLog & UsageLog): number {
  if (isAdmin.value) return row.account_stats_cost ?? row.total_cost ?? 0
  return row.actual_cost ?? row.total_cost ?? 0
}


</script>
<template>
<DataTable

          :columns="usageColumns"
          :data="rows"
          :loading="loading && !rows.length"
          row-key="id"
        >
          <template #cell-created_at="{ value }">
            <span class="whitespace-nowrap text-gray-600 dark:text-gray-300">{{ formatTime(value) }}</span>
          </template>

          <template #cell-account="{ row }">
            <div class="min-w-0 max-w-[200px]">
              <div class="truncate font-medium text-gray-900 dark:text-white" :title="accountLabel(row)">
                {{ accountLabel(row) }}
              </div>
            </div>
          </template>

          <template #cell-platform="{ row }">
            <span v-if="row.account_platform_snapshot" class="badge badge-gray">
              {{ row.account_platform_snapshot }}
            </span>
            <span v-else class="text-gray-400">{{ t('admin.pool.usage.noSnapshot') }}</span>
          </template>

          <template #cell-model="{ row }">
            <div class="min-w-0 max-w-[200px]">
              <div class="truncate text-gray-800 dark:text-gray-200" :title="row.model">{{ row.model }}</div>
              <div
                v-if="row.model_mapping_chain"
                class="mt-0.5 truncate font-mono text-xs text-gray-400"
                :title="row.model_mapping_chain"
              >
                {{ row.model_mapping_chain }}
              </div>
            </div>
          </template>

          <template #cell-upstream_model="{ row }">
            <span
              class="max-w-[180px] truncate font-mono text-xs"
              :class="
                row.upstream_model_mismatch
                  ? 'text-amber-600 dark:text-amber-400'
                  : 'text-gray-500 dark:text-gray-400'
              "
              :title="row.upstream_model || ''"
            >
              {{ row.upstream_model || '—' }}
            </span>
          </template>

          <template #cell-endpoint="{ row }">
            <span
              class="max-w-[180px] truncate font-mono text-xs text-gray-500 dark:text-gray-400"
              :title="row.inbound_endpoint || row.upstream_endpoint || ''"
            >
              {{ row.inbound_endpoint || row.upstream_endpoint || '—' }}
            </span>
          </template>

          <template #cell-reasoning_effort="{ row }">
            <span
              v-if="row.reasoning_effort"
              class="inline-flex items-center rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-600 dark:bg-gray-700 dark:text-gray-300"
            >{{ row.reasoning_effort }}</span>
            <span v-else class="text-gray-400">—</span>
          </template>

          <template #cell-group_name="{ row }">
            <span
              v-if="row.group?.name"
              class="inline-flex items-center rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-600 dark:bg-gray-700 dark:text-gray-300"
            >{{ row.group.name }}</span>
            <span v-else class="text-gray-400">—</span>
          </template>

          <template #cell-stream_type="{ row }">
            <span class="text-xs text-gray-600 dark:text-gray-300">
              {{ row.stream ? t('admin.pool.extra.streaming') : t('admin.pool.extra.nonStreaming') }}
            </span>
          </template>

          <template #cell-user_agent="{ row }">
            <span
              class="inline-block max-w-[150px] truncate align-middle font-mono text-xs text-gray-500 dark:text-gray-400"
              :title="row.user_agent || ''"
            >{{ row.user_agent || '—' }}</span>
          </template>

          <template #cell-ip_address="{ row }">
            <span class="whitespace-nowrap font-mono text-xs text-gray-500 dark:text-gray-400">{{ row.ip_address || '—' }}</span>
          </template>

          <template #cell-user="{ row }">
            <div class="min-w-0 max-w-[160px]">
              <div class="truncate text-gray-800 dark:text-gray-200" :title="row.user?.email || ''">
                {{ row.user?.email || row.user?.username || `#${row.user_id}` }}
              </div>
              <div class="mt-0.5 truncate font-mono text-xs text-gray-400">#{{ row.user_id }}</div>
            </div>
          </template>

          <template #cell-api_key="{ row }">
            <span
              class="max-w-[140px] truncate font-mono text-xs text-gray-500 dark:text-gray-400"
              :title="row.api_key?.name || ''"
            >
              {{ row.api_key?.name || `#${row.api_key_id}` }}
            </span>
          </template>

          <template #cell-tokens="{ row }">
            <span class="whitespace-nowrap font-mono tabular-nums text-gray-600 dark:text-gray-300">
              {{ formatNumber(row.input_tokens) }} / {{ formatNumber(row.output_tokens) }}
            </span>
          </template>

          <template #cell-cost="{ row }">
            <span class="whitespace-nowrap font-mono tabular-nums text-gray-800 dark:text-gray-200">
              {{ formatCostFixed(rowCost(row)) }}
            </span>
          </template>

          <template #cell-duration="{ row }">
            <span class="whitespace-nowrap text-gray-500 dark:text-gray-400">
              {{ row.duration_ms != null ? `${row.duration_ms} ms` : '—' }}
            </span>
          </template>

          <template #cell-request_id="{ value }">
            <span class="max-w-[180px] truncate font-mono text-xs text-gray-400" :title="value">{{ value }}</span>
          </template>

          <template #empty>
            <div class="flex flex-col items-center py-8">
              <Icon name="chart" size="xl" class="mb-4 h-12 w-12 text-gray-300 dark:text-dark-600" />
              <p class="text-sm font-medium text-gray-500 dark:text-gray-400">
                {{ failed ? t('admin.pool.networkError') : t('admin.pool.usage.empty') }}
              </p>
            </div>
          </template>
</DataTable>
</template>
