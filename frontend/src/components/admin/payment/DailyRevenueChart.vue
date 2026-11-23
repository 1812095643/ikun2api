<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler } from 'chart.js'
import { Line } from 'vue-chartjs'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { DailyPaymentStats, CurrencyAmounts } from '@/types/payment'
import { usePaymentChartTheme } from './usePaymentChartTheme'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler)
const props = defineProps<{ data: DailyPaymentStats[]; loading?: boolean }>()
const { t } = useI18n()
const theme = usePaymentChartTheme()
const metric = shallowRef<'amount' | 'net' | 'refund' | 'count'>('amount')
const metrics = computed(() => [
  { value: 'amount' as const, label: t('payment.admin.trendRevenue') },
  { value: 'net' as const, label: t('payment.admin.trendNet') },
  { value: 'refund' as const, label: t('payment.admin.trendRefund') },
  { value: 'count' as const, label: t('payment.admin.trendOrders') }
])
const colors = ['#14b8a6', '#3b82f6', '#f59e0b', '#ef4444']
const chartData = computed(() => {
  if (!props.data.length) return null
  const series = props.data.map(day => metric.value === 'count' ? {} : (day[metric.value] || {})) as CurrencyAmounts[]
  const currencies = [...new Set(series.flatMap(amounts => Object.keys(amounts)))].sort()
  const count = metric.value === 'count'
  return {
    labels: props.data.map(day => day.date),
    datasets: (count ? ['count'] : currencies).map((currency, index) => ({
      label: count ? t('payment.admin.paidOrders') : currency,
      data: count ? props.data.map(day => day.count) : series.map(amounts => amounts[currency] || 0),
      borderColor: colors[index % colors.length], backgroundColor: `${colors[index % colors.length]}18`,
      fill: true, tension: 0.25, pointRadius: props.data.length > 40 ? 0 : 2, pointHoverRadius: 4, borderWidth: 2
    }))
  }
})
const options = computed(() => ({
  responsive: true, maintainAspectRatio: false,
  interaction: { mode: 'index' as const, intersect: false },
  scales: {
    x: { grid: { display: false }, ticks: { maxTicksLimit: 8, color: theme.value.text } },
    y: { beginAtZero: metric.value !== 'net', grid: { color: theme.value.grid }, ticks: { color: theme.value.text, ...(metric.value === 'count' ? { precision: 0 } : {}) } }
  },
  plugins: { legend: { position: 'top' as const, display: metric.value !== 'count', labels: { color: theme.value.text } } }
}))
</script>
<template>
  <div class="card min-w-0 p-4">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('payment.admin.paymentTrend') }}</h3>
      <div class="flex gap-1 rounded-lg bg-gray-100 p-1 dark:bg-dark-700">
        <button v-for="item in metrics" :key="item.value" type="button" :aria-pressed="metric === item.value"
          class="rounded-md px-2 py-1 text-xs transition-colors"
          :class="metric === item.value ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300' : 'text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'"
          @click="metric = item.value">{{ item.label }}</button>
      </div>
    </div>
    <div class="h-56">
      <div v-if="loading" class="flex h-full items-center justify-center"><LoadingSpinner size="md" /></div>
      <Line v-else-if="chartData && chartData.datasets.length" :data="chartData" :options="options" />
      <div v-else class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400">{{ t('payment.admin.noData') }}</div>
    </div>
  </div>
</template>
