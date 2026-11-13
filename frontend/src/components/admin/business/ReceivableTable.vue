<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import DataTable from '@/components/common/DataTable.vue'
import { type ReceivableAccount } from '@/api/admin/business'
import { businessMoney } from '@/utils/business'
import { formatDateTime } from '@/utils/format'
defineProps<{ items: ReceivableAccount[]; loading: boolean }>()
defineEmits<{
  payment: [user: ReceivableAccount]
  statement: [user: ReceivableAccount]
}>()
const { t } = useI18n()
const columns = computed(() =>
  [
    'user',
    'outstanding',
    'credited',
    'received',
    'balance',
    'last_payment_at',
    'actions'
  ].map((key) => ({
    key,
    label: t(`business.${key === 'last_payment_at' ? 'lastPayment' : key}`)
  }))
)
</script>
<template>
  <DataTable
    :columns="columns"
    :data="items"
    :loading="loading"
    row-key="user_id"
    :sticky-first-column="true"
    :sticky-actions-column="true"
  >
    <template #cell-user="{ row }"
      ><div class="min-w-48">
        <div class="flex items-center gap-2">
          <span class="font-medium text-gray-900 dark:text-gray-100">{{
            row.username || row.email
          }}</span
          ><span v-if="row.is_downstream" class="badge badge-primary">{{
            t('business.downstream')
          }}</span
          ><span v-if="row.deleted" class="text-xs text-gray-400">{{
            t('business.deleted')
          }}</span>
        </div>
        <div v-if="row.username" class="mt-1 text-xs text-gray-500">
          {{ row.email }}
        </div>
      </div></template
    >
    <template #cell-outstanding="{ value }"
      ><span
        class="font-semibold tabular-nums"
        :class="
          Number(value) > 0
            ? 'text-amber-700 dark:text-amber-300'
            : 'text-gray-400'
        "
        >{{ businessMoney(value) }}</span
      ></template
    >
    <template #cell-credited="{ value }"
      ><span class="tabular-nums">{{ businessMoney(value) }}</span></template
    >
    <template #cell-received="{ value }"
      ><span class="tabular-nums">{{ businessMoney(value) }}</span></template
    >
    <template #cell-balance="{ value }"
      ><span class="tabular-nums">{{ businessMoney(value) }}</span></template
    >
    <template #cell-last_payment_at="{ value }">{{
      value ? formatDateTime(value) : '—'
    }}</template>
    <template #cell-actions="{ row }"
      ><div class="flex items-center gap-2">
        <button
          class="btn btn-primary btn-sm"
          :disabled="Number(row.outstanding) <= 0 || loading"
          @click="$emit('payment', row)"
        >
          {{ t('business.registerPayment') }}</button
        ><button
          class="btn btn-secondary btn-sm"
          :disabled="loading"
          @click="$emit('statement', row)"
        >
          {{ t('business.statement') }}
        </button>
      </div></template
    >
    <template #empty
      ><p class="py-10 text-center text-sm text-gray-400">
        {{ t('business.emptyDebt') }}
      </p></template
    >
  </DataTable>
</template>
