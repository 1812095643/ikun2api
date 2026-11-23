<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminPaymentAPI } from '@/api/admin/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import type { DashboardStats } from '@/types/payment'
import AppLayout from '@/components/layout/AppLayout.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import OrderStatsCards from '@/components/admin/payment/OrderStatsCards.vue'
import DailyRevenueChart from '@/components/admin/payment/DailyRevenueChart.vue'
import PaymentMethodChart from '@/components/admin/payment/PaymentMethodChart.vue'
import PaymentOrderStatusCard from '@/components/admin/payment/PaymentOrderStatusCard.vue'
import PaymentHourlyChart from '@/components/admin/payment/PaymentHourlyChart.vue'
import TopUsersLeaderboard from '@/components/admin/payment/TopUsersLeaderboard.vue'

const { t } = useI18n()
const app = useAppStore()
const today = new Date()
const firstDay = new Date(today)
firstDay.setDate(today.getDate() - 29)
const day = (date: Date) => `${date.getFullYear()}-${String(date.getMonth()+1).padStart(2,'0')}-${String(date.getDate()).padStart(2,'0')}`
const startDate = ref(day(firstDay)), endDate = ref(day(today))
const loading = ref(false)
const error = ref('')
const stats = ref<DashboardStats | null>(null)
let controller: AbortController | undefined

async function loadDashboard() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  error.value = ''
  try {
    const response = await adminPaymentAPI.getDashboard({
      start_date: startDate.value, end_date: endDate.value,
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone
    }, { signal: request.signal })
    if (!request.signal.aborted) stats.value = response.data
  } catch (err) {
    if (request.signal.aborted) return
    stats.value = null
    error.value = extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))
    app.showError(error.value)
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}
watch([startDate, endDate], loadDashboard, { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>

<template>
  <AppLayout>
    <div class="space-y-4" :aria-busy="loading">
      <div class="card flex flex-wrap items-center justify-between gap-3 px-4 py-3">
        <DateRangePicker v-model:start-date="startDate" v-model:end-date="endDate" />
        <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="loadDashboard">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />{{ t('common.refresh') }}
        </button>
      </div>
      <p v-if="error" class="rounded-lg bg-red-50 px-4 py-3 text-sm text-red-600 dark:bg-red-900/20 dark:text-red-300" role="alert">{{ error }}</p>
      <div v-if="loading && !stats" class="flex h-64 items-center justify-center"><LoadingSpinner /></div>
      <template v-if="stats">
        <OrderStatsCards :stats="stats" />
        <div class="grid grid-cols-1 gap-4 xl:grid-cols-3">
          <DailyRevenueChart class="xl:col-span-2" :data="stats.daily_series || []" :loading="loading" />
          <PaymentHourlyChart :data="stats.hourly_series || []" />
        </div>
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2 2xl:grid-cols-4">
          <PaymentMethodChart :methods="stats.payment_methods || []" />
          <PaymentMethodChart :methods="stats.order_types || []" kind="order" />
          <PaymentOrderStatusCard :items="stats.order_statuses || []" />
          <TopUsersLeaderboard :users="stats.top_users || {}" />
        </div>
      </template>
    </div>
  </AppLayout>
</template>
