<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { listPoolAccountRequests, type PoolAccount, type PoolScope } from '@/api/pool'
import type { AdminUsageLog, PaginatedResponse } from '@/types'
import { usePoolRequest } from '@/composables/usePoolRequest'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import PoolNetworkError from './PoolNetworkError.vue'
import { formatCostFixed, formatNumber, formatTime } from '@/utils/format'
const props = defineProps<{ account: PoolAccount | null; scope: PoolScope }>()
defineEmits<{ close: [] }>()
const { t } = useI18n()
const query = usePoolRequest<PaginatedResponse<AdminUsageLog> | null>(null)
const { loading, failed, updatedAt } = query
const requestPage = ref(1)
const requestPageSize = 20
const requestRows = computed(() => query.data.value?.items ?? [])
const requestsTotal = computed(() => query.data.value?.total ?? 0)
function rowCost(row: AdminUsageLog): number {
  return props.scope === 'admin' ? row.account_stats_cost ?? row.total_cost ?? 0 : row.actual_cost ?? row.total_cost ?? 0
}
function load() {
  if (!props.account) return
  const id = props.account.id, scope = props.scope, page = requestPage.value
  return query.run(signal => listPoolAccountRequests(id, { page, page_size: requestPageSize }, scope, { signal }))
}
function onRequestPageChange(page: number) { requestPage.value = page; query.reset(); void load() }
watch(() => [props.account?.id, props.scope], () => { query.reset(); requestPage.value = 1; void load() }, { immediate: true })

</script>
<template>
<BaseDialog :show="!!account" :title="account?.name || t('admin.pool.actions.requests')" width="extra-wide" :close-on-click-outside="true" @close="$emit('close')">
  <div class="space-y-4">
    <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.pool.dialog.subtitle') }}</p>
    <PoolNetworkError v-if="failed" :updated-at="updatedAt" :busy="loading" @retry="load" />
    <div v-if="loading && !requestRows.length" class="space-y-3 py-4" :aria-label="t('common.loading')">
      <div v-for="i in 5" :key="i" class="h-9 animate-pulse rounded bg-gray-100 dark:bg-dark-700" />
    </div>
    <p v-else-if="!requestRows.length && !failed" class="py-10 text-center text-sm text-gray-500 dark:text-dark-400">{{ t('admin.pool.dialog.empty') }}</p>
<div v-else-if="requestRows.length" class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <thead>
              <tr class="text-left text-xs font-semibold uppercase tracking-wider text-gray-400">
                <th class="whitespace-nowrap py-2 pr-4">{{ t('admin.pool.dialog.columns.time') }}</th>
                <th class="whitespace-nowrap py-2 pr-4">{{ t('admin.pool.dialog.columns.model') }}</th>
                <th class="whitespace-nowrap py-2 pr-4">{{ t('admin.pool.dialog.columns.endpoint') }}</th>
                <th class="whitespace-nowrap py-2 pr-4 text-right">
                  {{ t('admin.pool.dialog.columns.tokens') }}
                </th>
                <th class="whitespace-nowrap py-2 pr-4 text-right">
                  {{ t('admin.pool.dialog.columns.cost') }}
                </th>
                <th class="whitespace-nowrap py-2 pr-4 text-right">
                  {{ t('admin.pool.dialog.columns.duration') }}
                </th>
                <th class="whitespace-nowrap py-2">{{ t('admin.pool.dialog.columns.requestId') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
              <tr v-for="row in requestRows" :key="row.id">
                <td class="whitespace-nowrap py-2 pr-4 text-gray-600 dark:text-gray-300">
                  {{ formatTime(row.created_at) }}
                </td>
                <td class="max-w-[180px] truncate py-2 pr-4 text-gray-800 dark:text-gray-200" :title="row.model">
                  {{ row.model }}
                </td>
                <td
                  class="max-w-[160px] truncate py-2 pr-4 font-mono text-xs text-gray-500"
                  :title="row.inbound_endpoint || ''"
                >
                  {{ row.inbound_endpoint || '—' }}
                </td>
                <td
                  class="whitespace-nowrap py-2 pr-4 text-right font-mono tabular-nums text-gray-600 dark:text-gray-300"
                >
                  {{ formatNumber(row.input_tokens) }} / {{ formatNumber(row.output_tokens) }}
                </td>
                <td
                  class="whitespace-nowrap py-2 pr-4 text-right font-mono tabular-nums text-gray-800 dark:text-gray-200"
                >
                  {{ formatCostFixed(rowCost(row)) }}
                </td>
                <td class="whitespace-nowrap py-2 pr-4 text-right font-mono tabular-nums text-gray-500">
                  {{ row.duration_ms != null ? `${row.duration_ms} ms` : '—' }}
                </td>
                <td class="max-w-[200px] truncate py-2 font-mono text-xs text-gray-400" :title="row.request_id">
                  {{ row.request_id }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-if="requestsTotal > requestPageSize" class="flex justify-end">
          <Pagination
            :total="requestsTotal"
            :page="requestPage"
            :page-size="requestPageSize"
            :show-page-size-selector="false"
            @update:page="onRequestPageChange"
          />
        </div>

  </div>
</BaseDialog>
</template>
