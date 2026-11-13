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
    <template #actions
      ><div class="flex flex-wrap gap-x-10 gap-y-4 py-1">
        <div
          v-for="key in [
            'outstanding',
            'received',
            'credited',
            'debtors'
          ] as const"
          :key="key"
        >
          <div class="text-xs font-medium text-gray-500">
            {{ t(`business.${key}`) }}
          </div>
          <div
            class="mt-1 text-xl font-semibold tabular-nums"
            :class="
              key === 'outstanding'
                ? 'text-amber-700 dark:text-amber-300'
                : 'text-gray-900 dark:text-gray-100'
            "
          >
            {{
              !data || loading
                ? '—'
                : key === 'debtors'
                  ? data.summary[key]
                  : businessMoney(data.summary[key])
            }}
          </div>
        </div>
      </div></template
    >
    <template #filters
      ><div class="flex flex-wrap items-center gap-3">
        <div class="relative w-full sm:w-72">
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
        <div class="w-36">
          <Select
            v-model="filter.state"
            :options="states"
            :searchable="false"
            :aria-label="t('common.status')"
          />
        </div>
        <button
          class="btn btn-secondary sm:ml-auto"
          :disabled="loading"
          @click="reload"
        >
          <Icon name="refresh" size="sm" />{{ t('business.refresh') }}
        </button>
      </div></template
    >
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
  <ReceivablePaymentDialog
    :user="paymentUser"
    @close="paymentUser = null"
    @saved="reload"
  />
  <ReceivableHistoryDialog :user="historyUser" @close="historyUser = null" />
</template>
