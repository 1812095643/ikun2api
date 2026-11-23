<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CurrencyAmounts, DashboardStats } from '@/types/payment'

const props = defineProps<{ stats: DashboardStats }>()
const { t, locale } = useI18n()
const cards = computed(() => [
  { key: 'revenue', label: t('payment.admin.periodRevenue'), amounts: props.stats.total_amount, detail: t('payment.admin.paidOrdersCount', { count: props.stats.total_count }), previous: props.stats.previous_amount },
  { key: 'net', label: t('payment.admin.netRevenue'), amounts: props.stats.net_amount, detail: t('payment.admin.netRevenueHint') },
  { key: 'orders', label: t('payment.admin.paidOrders'), value: props.stats.total_count.toLocaleString(), detail: t('payment.admin.previousOrders', { count: props.stats.previous_count ?? '—' }) },
  { key: 'users', label: t('payment.admin.payingUsers'), value: props.stats.paying_users?.toLocaleString() ?? '—', detail: t('payment.admin.repeatPayers', { count: props.stats.repeat_payers ?? '—' }) },
  { key: 'created', label: t('payment.admin.createdOrders'), value: props.stats.order_count?.toLocaleString() ?? '—', detail: t('payment.admin.pendingOrdersCount', { count: props.stats.pending_orders ?? '—' }) },
  { key: 'conversion', label: t('payment.admin.paidOrderRate'), value: props.stats.paid_order_rate == null ? '—' : `${props.stats.paid_order_rate}%`, detail: t('payment.admin.paidOrderRateHint') },
  { key: 'average', label: t('payment.admin.avgAmount'), amounts: props.stats.avg_amount },
  { key: 'refund', label: t('payment.admin.completedRefundAmount'), amounts: props.stats.refund_amount, detail: t('payment.admin.refundOrdersCount', { count: props.stats.refund_count ?? '—' }) }
])

function amounts(value?: CurrencyAmounts): [string, number][] {
  return Object.entries(value || {}).sort(([a], [b]) => a.localeCompare(b))
}
function money(currency: string, amount: number) {
  return new Intl.NumberFormat(locale.value, { style: 'currency', currency }).format(amount)
}
function change(current: number, previous?: number) {
  if (previous == null || previous === 0) return '—'
  const percent = (current - previous) / previous * 100
  return `${percent > 0 ? '+' : ''}${percent.toFixed(1)}%`
}
</script>

<template>
  <div class="grid grid-cols-2 gap-3 xl:grid-cols-4">
    <div v-for="card in cards" :key="card.key" class="card min-w-0 px-4 py-3" :data-testid="`payment-stat-${card.key}`">
      <p class="text-xs font-medium text-gray-500 dark:text-gray-400" :title="card.detail">{{ card.label }}</p>
      <div v-if="card.amounts && amounts(card.amounts).length" class="mt-1 space-y-0.5">
        <div v-for="[currency, amount] in amounts(card.amounts)" :key="currency" class="flex flex-wrap items-baseline justify-between gap-x-2">
          <span class="text-xl font-semibold tabular-nums tracking-tight text-gray-900 dark:text-white">{{ money(currency, amount) }}</span>
          <span v-if="card.previous" class="text-xs tabular-nums text-gray-500 dark:text-gray-400" :title="t('payment.admin.previousPeriod')">{{ change(amount, card.previous[currency]) }}</span>
        </div>
      </div>
      <p v-else class="mt-1 text-xl font-semibold tabular-nums tracking-tight text-gray-900 dark:text-white">{{ card.value ?? (card.amounts ? '0' : '—') }}</p>
      <p v-if="card.detail && !['net', 'conversion'].includes(card.key)" class="mt-1 truncate text-xs text-gray-500 dark:text-gray-400">{{ card.detail }}</p>
    </div>
  </div>
</template>
