<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import DataTable from '@/components/common/DataTable.vue'
import type { MonitorDelivery, NotificationTarget, NotificationChannel } from '@/api/admin/opsMonitor'
const props = defineProps<{ items: MonitorDelivery[]; targets?: NotificationTarget[]; channels?: NotificationChannel[]; loading?: boolean }>()
const { t, te } = useI18n()
const columns = computed(() => ['target', 'kind', 'state', 'attempts', 'next_attempt'].map(key => ({ key, label: t(`admin.ops.enhanced.deliveryColumns.${key}`) })))
function target(id: string) { const item = props.targets?.find(target => target.id === id); const channel = props.channels?.find(channel => channel.id === item?.channel_id); return item ? `${channel?.name || ''} · ${item.name}` : id }
</script>
<template>
  <DataTable :data="items" :columns="columns" :loading="loading" row-key="id">
    <template #cell-target="{ row }"><span class="max-w-52 truncate text-xs">{{ target(row.target) }}</span></template>
    <template #cell-kind="{ row }"><span class="text-xs">{{ te(`admin.ops.enhanced.notificationKinds.${row.kind}`) ? t(`admin.ops.enhanced.notificationKinds.${row.kind}`) : row.kind }}</span></template>
    <template #cell-state="{ row }"><span class="text-xs" :class="row.state === 'accepted' ? 'text-emerald-600' : ['dead', 'uncertain'].includes(row.state) ? 'text-amber-600' : 'text-gray-500'">{{ t(`admin.ops.enhanced.deliveryState.${row.state}`) }}</span><p v-if="row.error_class" class="mt-0.5 max-w-64 text-xs text-gray-400">{{ t(`admin.ops.enhanced.deliveryError.${row.error_class}`) }}</p></template>
    <template #cell-attempts="{ row }"><span class="text-xs tabular-nums">{{ row.attempts }}</span></template>
    <template #cell-next_attempt="{ row }"><span class="whitespace-nowrap text-xs tabular-nums">{{ new Date(row.next_attempt).toLocaleString() }}</span></template>
  </DataTable>
</template>
