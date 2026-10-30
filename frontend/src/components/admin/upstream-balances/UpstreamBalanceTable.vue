<script setup lang="ts">
import DataTable from '@/components/common/DataTable.vue'
import Icon from '@/components/icons/Icon.vue'
import { balanceComparisonLabel, balanceKindLabel, balanceScopeLabel, formatBalanceTime, formatUpstreamBalance, type UpstreamBalance } from '@/api/upstreamBalances'

defineProps<{ items: UpstreamBalance[]; loading: boolean; refreshing: string | null }>()
const emit = defineEmits<{ edit: [item: UpstreamBalance]; details: [item: UpstreamBalance]; refresh: [id: string] }>()
const columns = [
  { key: 'name', label: '供应商', sortable: true },
  { key: 'balance', label: '余额' },
  { key: 'comparison', label: 'Key 校验' },
  { key: 'monitor', label: '余额监控' },
  { key: 'last_checked_at', label: '最近查询', sortable: true },
  { key: 'actions', label: '操作' }
]
function rowKind(row: UpstreamBalance) {
  const kinds = [...new Set(row.snapshot.readings.filter(r => !r.error).map(r => r.kind))]
  return kinds.length === 1 ? balanceKindLabel(kinds[0]) : balanceKindLabel(row.config.kind)
}
</script>

<template>
  <DataTable :columns="columns" :data="items" :loading="loading" row-key="id" :expandable-actions="false">
    <template #cell-name="{ row }">
      <div class="min-w-[150px] max-w-[260px]">
        <a :href="row.url" target="_blank" rel="noopener noreferrer" class="inline-flex max-w-full items-center gap-1 font-medium text-primary-600 hover:underline dark:text-primary-400" :title="row.url"><span class="truncate">{{ row.name }}</span><Icon name="externalLink" size="sm" class="shrink-0" /></a>
        <div class="mt-1 truncate text-xs text-gray-500 dark:text-dark-400" :title="row.url">{{ row.url }}</div>
        <div class="mt-1 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-dark-400"><span>{{ rowKind(row) }}</span><span>·</span><span>{{ row.accounts.length }} 个账号 / {{ row.key_count }} 个 Key</span></div>
      </div>
    </template>
    <template #cell-balance="{ row }">
      <div class="min-w-[110px]">
        <div class="font-semibold tabular-nums" :class="row.alert.low ? 'text-red-600 dark:text-red-400' : 'text-gray-900 dark:text-gray-100'">{{ formatUpstreamBalance(row.snapshot.balance, row.snapshot.unit, row.snapshot.unlimited) }}</div>
        <div class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ row.snapshot.scope ? balanceScopeLabel(row.snapshot.scope) : (row.last_checked_at ? '查看明细' : '等待查询') }}</div>
        <div v-if="row.alert.low" class="mt-1 text-xs text-red-600 dark:text-red-400">余额偏低</div>
      </div>
    </template>
    <template #cell-comparison="{ row }">
      <div class="min-w-[90px]">
        <span v-if="row.checking || row.queued" class="inline-flex items-center gap-1.5 text-sm text-primary-600 dark:text-primary-400"><Icon v-if="row.checking" name="refresh" size="sm" class="animate-spin" />{{ row.checking ? '查询中' : '已排队' }}</span>
        <span v-else-if="!row.last_checked_at" class="text-sm text-gray-400 dark:text-dark-500">未校验</span>
        <button v-else type="button" class="text-sm hover:underline" :class="['different', 'partial', 'unverified'].includes(row.snapshot.comparison) ? 'text-amber-600 dark:text-amber-400' : 'text-gray-700 dark:text-dark-200'" @click="emit('details', row)">{{ row.token_configured && row.config.kind !== 'sub2api' && row.snapshot.total === 1 ? '钱包令牌' : balanceComparisonLabel(row.snapshot.comparison) }}</button>
        <div v-if="row.last_checked_at" class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ row.snapshot.successful }}/{{ row.snapshot.total }} 查询成功</div>
      </div>
    </template>
    <template #cell-monitor="{ row }">
      <div v-if="row.config.monitor_enabled" class="min-w-[100px] text-xs text-gray-500 dark:text-dark-400"><span class="badge badge-primary mb-1">已启用</span><div>{{ row.config.interval_minutes }} 分钟 / 次</div><div>低于 {{ row.config.threshold }} {{ row.snapshot.unit }}</div><div v-if="row.alert.error" class="mt-1 max-w-[150px] text-amber-600 dark:text-amber-400" :title="row.alert.error">{{ row.alert.error.includes('邮件') ? '邮件发送异常' : '请检查监控来源' }}</div></div>
      <span v-else class="text-sm text-gray-400 dark:text-dark-500">未启用</span>
    </template>
    <template #cell-last_checked_at="{ row }"><div class="whitespace-nowrap text-xs text-gray-500 dark:text-dark-400" :title="row.last_checked_at || ''">{{ formatBalanceTime(row.last_checked_at) }}</div></template>
    <template #cell-actions="{ row }"><div class="flex flex-wrap items-center gap-1.5"><button type="button" class="btn btn-secondary btn-sm" :disabled="!!refreshing || row.checking || row.queued" @click="emit('refresh', row.id)">查询</button><button type="button" class="btn btn-secondary btn-sm" @click="emit('edit', row)">设置</button><button type="button" class="btn btn-ghost btn-sm" @click="emit('details', row)">明细</button></div></template>
    <template #empty><div class="flex flex-col items-center py-6"><Icon name="server" size="xl" class="mb-3 text-gray-400 dark:text-dark-500" /><p class="font-medium text-gray-700 dark:text-dark-200">暂无 API Key 上游</p><p class="mt-1 text-sm text-gray-500 dark:text-dark-400">账号管理中配置的 API Key 上游将在此汇总。</p><RouterLink to="/admin/accounts" class="btn btn-secondary mt-4">账号管理</RouterLink></div></template>
  </DataTable>
</template>
