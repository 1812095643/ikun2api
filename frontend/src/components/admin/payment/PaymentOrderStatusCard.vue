<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import OrderStatusBadge from '@/components/payment/OrderStatusBadge.vue'
import type { OrderStatus } from '@/types/payment'
const props = defineProps<{ items: { status: string; count: number }[] }>()
const { t } = useI18n()
const total = computed(() => props.items.reduce((sum, item) => sum + item.count, 0))
const sorted = computed(() => [...props.items].sort((a,b) => b.count - a.count || a.status.localeCompare(b.status)))
</script>
<template>
  <div class="card min-w-0 p-4">
    <h3 class="mb-4 text-sm font-semibold text-gray-900 dark:text-white">{{ t('payment.admin.orderStatusDistribution') }}</h3>
    <div v-if="items.length" class="max-h-80 space-y-3 overflow-y-auto">
      <div v-for="item in sorted" :key="item.status" class="flex items-center justify-between gap-2">
        <OrderStatusBadge :status="item.status as OrderStatus" />
        <span class="whitespace-nowrap text-sm tabular-nums text-gray-900 dark:text-gray-100">{{ item.count.toLocaleString() }}<span class="ml-2 text-xs text-gray-500 dark:text-gray-400">{{ (item.count / total * 100).toFixed(1) }}%</span></span>
      </div>
    </div>
    <div v-else class="flex h-32 items-center justify-center text-sm text-gray-500 dark:text-gray-400">{{ t('payment.admin.noData') }}</div>
  </div>
</template>
