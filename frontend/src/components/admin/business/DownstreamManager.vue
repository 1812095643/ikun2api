<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import Select from '@/components/common/Select.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import BusinessError from './BusinessError.vue'
import DownstreamTable from './DownstreamTable.vue'
import DownstreamActionDialog, {
  type DownstreamAction
} from './DownstreamActionDialog.vue'
import DownstreamUsageDialog from './DownstreamUsageDialog.vue'
import ReceivableHistoryDialog from './ReceivableHistoryDialog.vue'
import UserBalanceModal from '@/components/admin/user/UserBalanceModal.vue'
import { adminAPI } from '@/api/admin'
import {
  businessAPI,
  type DownstreamUser,
  type DownstreamPage,
  type ReceivableAccount
} from '@/api/admin/business'

import { useBusinessRequest } from '@/composables/useBusinessRequest'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { AdminUser } from '@/types'
const { t } = useI18n()
const app = useAppStore()
function day(value: Date) {
  return `${value.getFullYear()}-${String(value.getMonth() + 1).padStart(2, '0')}-${String(value.getDate()).padStart(2, '0')}`
}
const today = new Date()
const start = new Date(today)
start.setDate(today.getDate() - 29)
const filter = reactive({
  search: '',
  page: 1,
  page_size: 20,
  from: day(start),
  to: day(today)
})
const preset = ref('last30')
const search = ref('')
const selected = ref<number[]>([])
const mode = ref<DownstreamAction | null>(null)
const targetIDs = ref<number[]>([])
const initialGroup = ref<number | null>(null)
const usageUser = ref<DownstreamUser | null>(null)
const debtUser = ref<ReceivableAccount | null>(null)
const balanceUser = ref<AdminUser | null>(null)
const creditLoading = ref(false)
const { data, loading, error, load } = useBusinessRequest<DownstreamPage>()

const presets = computed(() =>
  ['today', 'last7', 'last30'].map((value) => ({
    value,
    label: t(`business.${value}`)
  }))
)
const invalidRange = computed(
  () => !filter.from || !filter.to || filter.from > filter.to
)
const range = computed(() => {
  if (invalidRange.value) return {}
  const from = new Date(`${filter.from}T00:00:00`)
  const to = new Date(`${filter.to}T00:00:00`)
  to.setDate(to.getDate() + 1)
  return { from: from.toISOString(), to: to.toISOString() }
})
function updatePageSize(size: number) {
  filter.page_size = size
  filter.page = 1
}
function reload() {
  selected.value = []
  if (invalidRange.value) return
  const params = {
    search: filter.search,
    page: filter.page,
    page_size: filter.page_size,
    ...range.value
  }
  void load((signal) => businessAPI.downstreams(params, signal))
}
watch(preset, (value) => {
  const current = new Date()
  const from = new Date(current)
  from.setDate(
    current.getDate() - (value === 'last30' ? 29 : value === 'last7' ? 6 : 0)
  )
  filter.from = day(from)
  filter.to = day(current)
  filter.page = 1
})
let timer: ReturnType<typeof setTimeout> | undefined
watch(search, (value) => {
  clearTimeout(timer)
  timer = setTimeout(() => {
    filter.page = 1
    filter.search = value.trim()
  }, 250)
})
watch(
  () => [filter.from, filter.to],
  () => {
    filter.page = 1
  }
)
watch(() => ({ ...filter }), reload, { immediate: true })
onBeforeUnmount(() => clearTimeout(timer))
function action(value: DownstreamAction, row?: DownstreamUser, group?: number) {
  targetIDs.value = row ? [row.id] : [...selected.value]
  initialGroup.value = group || null
  mode.value = value
}
async function credit(row: DownstreamUser) {
  if (creditLoading.value) return
  creditLoading.value = true
  try {
    balanceUser.value = await adminAPI.users.getById(row.id)
  } catch (e) {
    app.showError(extractApiErrorMessage(e, t('common.error')))
  } finally {
    creditLoading.value = false
  }
}
function debt(row: DownstreamUser) {
  debtUser.value = {
    user_id: row.id,
    email: row.email,
    username: row.username,
    deleted: false,
    is_downstream: true,
    balance: row.balance,
    outstanding: row.outstanding,
    credited: '',
    received: ''
  }
}
</script>
<template>
  <TablePageLayout>
    <template #actions
      ><div class="flex flex-wrap items-center justify-between gap-3">
        <span class="text-sm text-gray-500"
          >{{ t('business.countUsers') }}
          <strong class="ml-1 text-gray-900 dark:text-gray-100">{{
            data && !loading ? data.total : '—'
          }}</strong></span
        >
        <div class="flex flex-wrap gap-2">
          <router-link to="/admin/receivables" class="btn btn-secondary">{{
            t('business.ledger')
          }}</router-link
          ><button class="btn btn-primary" @click="action('add')">
            <Icon name="plus" size="sm" />{{ t('business.addDownstream') }}
          </button>
        </div>
      </div></template
    >
    <template #filters
      ><div class="space-y-3">
        <div class="flex flex-wrap items-center gap-3">
          <div class="relative w-full sm:w-60">
            <Icon
              name="search"
              size="sm"
              class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"
            /><input
              v-model="search"
              class="input pl-9"
              :placeholder="t('business.searchUser')"
              :aria-label="t('business.searchUser')"
            />
          </div>
          <div class="w-36">
            <Select
              v-model="preset"
              :options="presets"
              :searchable="false"
              :aria-label="t('business.date')"
            />
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <input
              v-model="filter.from"
              type="date"
              class="input w-40"
              :aria-label="t('business.from')"
            /><span class="text-gray-400">—</span
            ><input
              v-model="filter.to"
              type="date"
              class="input w-40"
              :aria-label="t('business.to')"
            />
          </div>
          <button
            class="btn btn-secondary ml-auto"
            :disabled="loading || invalidRange"
            :aria-label="t('business.refresh')"
            @click="reload"
          >
            <Icon name="refresh" size="sm" />
          </button>
        </div>
        <div
          v-if="selected.length"
          class="flex flex-wrap items-center gap-2 rounded-xl bg-primary-50 px-3 py-2 dark:bg-primary-900/20"
        >
          <span class="mr-3 text-sm text-primary-700 dark:text-primary-300">{{
            t('business.selected', { count: selected.length })
          }}</span
          ><button
            class="btn btn-secondary btn-sm"
            :disabled="loading"
            @click="action('price')"
          >
            {{ t('business.pricing') }}</button
          ><button
            class="btn btn-ghost btn-sm text-red-600"
            :disabled="loading"
            @click="action('remove')"
          >
            {{ t('business.removeDownstream') }}
          </button>
        </div>
      </div></template
    >
    <template #table
      ><BusinessError
        v-if="error"
        class="m-4"
        :message="error"
        @retry="reload" />
      <div
        v-else-if="invalidRange"
        class="p-8 text-center text-sm text-gray-500"
      >
        {{ t('business.from') }} ≤ {{ t('business.to') }}
      </div>
      <DownstreamTable
        v-else
        :items="data?.items || []"
        :loading="loading || creditLoading"
        :selected="selected"
        @update:selected="selected = $event.map(Number)"
        @price="(row, group) => action('price', row, group)"
        @usage="usageUser = $event"
        @credit="credit"
        @debt="debt"
    /></template>
    <template #pagination
      ><Pagination
        v-if="data && !error && !invalidRange"
        :page="filter.page"
        :page-size="filter.page_size"
        :total="data.total"
        @update:page="filter.page = $event"
        @update:page-size="updatePageSize"
    /></template>
  </TablePageLayout>
  <DownstreamActionDialog
    :mode="mode"
    :user-ids="targetIDs"
    :initial-group="initialGroup"
    @close="mode = null"
    @saved="reload"
  />
  <DownstreamUsageDialog
    :user="usageUser"
    :range="range"
    @close="usageUser = null"
  />
  <ReceivableHistoryDialog :user="debtUser" @close="debtUser = null" />
  <UserBalanceModal
    :show="!!balanceUser"
    :user="balanceUser"
    operation="add"
    @close="balanceUser = null"
    @success="reload"
  />
</template>
