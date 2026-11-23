<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, CategoryScale, LinearScale, BarElement, Tooltip } from 'chart.js'
import { Bar } from 'vue-chartjs'
import { usePaymentChartTheme } from './usePaymentChartTheme'

ChartJS.register(CategoryScale, LinearScale, BarElement, Tooltip)
const props = defineProps<{ data: { hour: number; count: number }[] }>()
const { t } = useI18n()
const theme = usePaymentChartTheme()
const chartData = computed(() => ({
  labels: props.data.map(item => `${String(item.hour).padStart(2, '0')}:00`),
  datasets: [{ label: t('payment.admin.paidOrders'), data: props.data.map(item => item.count), backgroundColor: 'rgba(20,184,166,0.75)', borderRadius: 3, maxBarThickness: 16 }]
}))
const options = computed(() => ({
  responsive: true, maintainAspectRatio: false,
  scales: { x: { grid: { display: false }, ticks: { maxTicksLimit: 6, color: theme.value.text } }, y: { beginAtZero: true, grid: {color:theme.value.grid}, ticks: { precision: 0, color:theme.value.text } } },
  plugins: { legend: { display: false } }
}))
</script>
<template>
  <div class="card min-w-0 p-4">
    <h3 class="mb-4 text-sm font-semibold text-gray-900 dark:text-white">{{ t('payment.admin.hourlyPayments') }}</h3>
    <div class="h-56">
      <Bar v-if="data.some(item => item.count > 0)" :data="chartData" :options="options" />
      <div v-else class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400">{{ t('payment.admin.noData') }}</div>
    </div>
  </div>
</template>
