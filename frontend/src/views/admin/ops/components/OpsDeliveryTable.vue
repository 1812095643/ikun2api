<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import DataTable from '@/components/common/DataTable.vue'
import type { MonitorDelivery, NotificationTarget, NotificationChannel } from '@/api/admin/opsMonitor'
import { incidentTime } from '../utils/incidentPresentation'
const props = defineProps<{ items: MonitorDelivery[]; targets?: NotificationTarget[]; channels?: NotificationChannel[]; loading?: boolean }>()
const { t, te } = useI18n()
const columns = computed(() => ['target', 'kind', 'state', 'attempts', 'accepted_at', 'next_attempt'].map(key => ({ key, label: t(`admin.ops.enhanced.deliveryColumns.${key}`) })))
function target(row: MonitorDelivery) { const item = props.targets?.find(target => target.id === row.target); const channel = props.channels?.find(channel => channel.id === (item?.channel_id || row.channel)); return [row.channel_name || channel?.name, row.target_name || item?.name || t('admin.ops.enhanced.removedTarget')].filter(Boolean).join(' · ') }
</script>
<template>
  <DataTable :data="items" :columns="columns" :loading="loading" row-key="id">
    <template #cell-target="{ row }"><div class="max-w-52 truncate text-xs">{{ target(row) }}</div><div class="max-w-52 truncate text-xs text-gray-400" :title="row.target">{{ row.target }}</div></template>
    <template #cell-kind="{ row }"><span class="text-xs">{{ te(`admin.ops.enhanced.notificationKinds.${row.kind}`) ? t(`admin.ops.enhanced.notificationKinds.${row.kind}`) : row.kind }}</span></template>
    <template #cell-state="{ row }"><span class="text-xs" :class="row.state === 'accepted' ? 'text-emerald-600' : ['dead', 'uncertain'].includes(row.state) ? 'text-amber-600' : 'text-gray-500'">{{ t(`admin.ops.enhanced.deliveryState.${row.state}`) }}</span><p v-if="row.error_class" class="mt-0.5 max-w-64 text-xs text-gray-400">{{ t(`admin.ops.enhanced.deliveryError.${row.error_class}`) }}</p></template>
    <template #cell-attempts="{ row }"><span class="text-xs tabular-nums">{{ row.attempts }}</span></template>
    <template #cell-accepted_at="{ row }"><span class="whitespace-nowrap text-xs tabular-nums">{{ incidentTime(row.accepted_at) }}</span></template>
    <template #cell-next_attempt="{ row }"><span class="whitespace-nowrap text-xs tabular-nums">{{ incidentTime(row.next_attempt) }}</span></template>
  </DataTable>
</template>
