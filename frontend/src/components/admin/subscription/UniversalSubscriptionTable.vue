<script setup lang="ts">
import { computed } from 'vue'
import DataTable from '@/components/common/DataTable.vue'
import type { Column } from '@/components/common/types'
import { subscriptionMoney as money, subscriptionTime as time, subscriptionStatusLabels as labels, type UniversalSubscription } from '@/api/universalSubscriptions'
defineProps<{ items: UniversalSubscription[]; loading: boolean }>()
const emit = defineEmits<{ records: [row: UniversalSubscription]; edit: [row: UniversalSubscription]; status: [row: UniversalSubscription, status: string] }>()
const columns = computed<Column[]>(() => [{ key: 'user_email', label: '用户' }, { key: 'name', label: '套餐' }, { key: 'remaining_usd', label: '额度' }, { key: 'periods', label: '周期使用' }, { key: 'expires_at', label: '有效期' }, { key: 'status', label: '状态' }, { key: 'actions', label: '操作' }])
</script>
<template>
 <DataTable :columns="columns" :data="items" :loading="loading">
  <template #cell-user_email="{ row }"><div class="text-sm"><RouterLink :to="{ path: '/admin/usage', query: { user_id: row.user_id } }" class="text-primary-600 hover:underline dark:text-primary-400">{{ row.user_email || `User #${row.user_id}` }}</RouterLink><span class="mt-1 block text-xs text-gray-500">ID {{ row.user_id }} · {{ row.user_name }}</span></div></template>
  <template #cell-name="{ row }"><span class="font-medium">{{ row.name }}</span><span class="mt-1 block text-xs text-gray-500">{{ row.order_id ? `订单 #${row.order_id}` : '管理员 / 兑换发放' }}</span></template>
  <template #cell-remaining_usd="{ row }"><span class="font-semibold tabular-nums">{{ money(row.remaining_usd) }} / {{ money(row.total_credit_usd) }}</span><span class="mt-1 block text-xs text-gray-500">已用 {{ money(row.total_used_usd) }}<template v-if="row.held_usd"> · 预留 {{ money(row.held_usd) }}</template></span></template>
  <template #cell-periods="{ row }"><div class="space-y-1 text-xs tabular-nums"><p>日 {{ money(row.daily_used_usd) }} / {{ money(row.daily_limit_usd) }}</p><p>周 {{ money(row.weekly_used_usd) }} / {{ money(row.weekly_limit_usd) }}</p><p>月 {{ money(row.monthly_used_usd) }} / {{ money(row.monthly_limit_usd) }}</p></div></template>
  <template #cell-expires_at="{ row }"><div class="text-xs"><p>{{ time(row.starts_at) }}</p><p class="mt-1 text-gray-500">至 {{ time(row.expires_at) }}</p></div></template>
  <template #cell-status="{ row }"><span class="badge" :class="row.status === 'active' ? 'badge-success' : 'badge-gray'">{{ labels[row.status as UniversalSubscription['status']] }}</span></template>
  <template #cell-actions="{ row }"><div class="flex flex-wrap gap-3 text-xs"><button class="text-primary-600 dark:text-primary-400" @click="emit('records', row)">记录</button><button v-if="row.status !== 'revoked' && row.status !== 'expired'" class="text-primary-600" @click="emit('edit', row)">调整</button><button v-if="row.status === 'active' || row.status === 'scheduled'" class="text-amber-600" @click="emit('status', row, 'suspended')">停用</button><button v-if="row.status === 'suspended'" class="text-primary-600" @click="emit('status', row, 'active')">恢复</button><button v-if="row.status !== 'revoked' && row.status !== 'expired'" class="text-red-600" @click="emit('status', row, 'revoked')">撤销</button></div></template>
 </DataTable>
</template>
