<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import Select from '@/components/common/Select.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import BusinessError from './BusinessError.vue'
import ReceivableTable from './ReceivableTable.vue'
import ReceivablePaymentDialog from './ReceivablePaymentDialog.vue'
import ReceivableHistoryDialog from './ReceivableHistoryDialog.vue'
import ReceivableCreditDialog from './ReceivableCreditDialog.vue'
import {
  businessAPI,
  type ReceivableAccount,
  type ReceivablePage
} from '@/api/admin/business'
import { businessMoney } from '@/utils/business'
import { useBusinessRequest } from '@/composables/useBusinessRequest'
const { t } = useI18n()
const filter = reactive({ search: '', state: 'open', page: 1, page_size: 20 })
const search = ref('')
const adding = ref(false)
const paymentUser = ref<ReceivableAccount | null>(null)
const historyUser = ref<ReceivableAccount | null>(null)
const { data, loading, error, load } = useBusinessRequest<ReceivablePage>()
const states = computed(() =>
  [
    ['open', 'open'],
    ['settled', 'settled'],
    ['all', 'allAccounts']
  ].map(([value, label]) => ({ value, label: t(`business.${label}`) }))
)
function updatePageSize(size: number) {
  filter.page_size = size
  filter.page = 1
}
function reload() {
  const params = { ...filter }
  void load((signal) => businessAPI.receivables(params, signal))
}
let timer: ReturnType<typeof setTimeout> | undefined
watch(search, (value) => {
  clearTimeout(timer)
  timer = setTimeout(() => {
    filter.page = 1
    filter.search = value.trim()
  }, 250)
})
watch(
  () => filter.state,
  () => {
    filter.page = 1
  }
)
watch(() => ({ ...filter }), reload, { immediate: true })
onBeforeUnmount(() => clearTimeout(timer))
</script>
<template>
  <TablePageLayout>
    <template #filters>
      <div class="flex flex-wrap items-center justify-between gap-x-6 gap-y-4 rounded-xl bg-white px-5 py-4 shadow-sm ring-1 ring-gray-200/70 dark:bg-dark-800 dark:ring-dark-700">
      <dl class="flex flex-wrap items-center gap-y-3 divide-x divide-gray-200 dark:divide-dark-600">
        <div
          v-for="key in [
            'outstanding',
            'received',
            'credited',
            'debtors'
          ] as const"
          :key="key"
          class="px-5 first:pl-0 last:pr-0"
        >
          <dt class="text-[11px] font-medium text-gray-500 dark:text-gray-400">
            {{ t(`business.${key}`) }}
          </dt>
          <dd
            class="mt-0.5 text-lg font-semibold leading-6 tracking-tight tabular-nums"
            :class="
              key === 'outstanding'
                ? 'text-amber-700 dark:text-amber-300'
                : 'text-gray-900 dark:text-gray-100'
            "
          >
            {{
              !data || loading || error
                ? '—'
                : key === 'debtors'
                  ? data.summary[key]
                  : businessMoney(data.summary[key])
            }}
          </dd>
        </div>
      </dl>
      <div class="flex w-full flex-wrap items-center gap-2 xl:ml-auto xl:w-auto">
        <div class="relative min-w-40 flex-1 sm:w-56 sm:flex-none">
          <Icon
            name="search"
            size="sm"
            class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"
          /><input
            v-model="search"
            :placeholder="t('business.searchUser')"
            :aria-label="t('business.searchUser')"
            class="input pl-9"
          />
        </div>
        <div class="w-32">
          <Select
            v-model="filter.state"
            :options="states"
            :searchable="false"
            :aria-label="t('common.status')"
          />
        </div>
        <button class="btn btn-primary gap-1.5" @click="adding = true">
          <Icon name="plus" size="sm" />{{ t('business.addCredit') }}
        </button>
        <button
          class="btn btn-secondary px-2.5"
          :title="t('business.refresh')"
          :aria-label="t('business.refresh')"
          :disabled="loading"
          @click="reload"
        >
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
        </button>
      </div>
      </div>
    </template>
    <template #table
      ><BusinessError
        v-if="error"
        class="m-4"
        :message="error"
        @retry="reload" /><ReceivableTable
        v-else
        :items="data?.items || []"
        :loading="loading"
        @payment="paymentUser = $event"
        @statement="historyUser = $event"
    /></template>
    <template #pagination
      ><Pagination
        v-if="data && !error"
        :page="filter.page"
        :page-size="filter.page_size"
        :total="data.total"
        @update:page="filter.page = $event"
        @update:page-size="updatePageSize"
    /></template>
  </TablePageLayout>
  <ReceivableCreditDialog v-if="adding" @close="adding = false" @saved="reload" />
  <ReceivablePaymentDialog
    :user="paymentUser"
    @close="paymentUser = null"
    @saved="reload"
  />
  <ReceivableHistoryDialog :user="historyUser" @close="historyUser = null" />
</template>
