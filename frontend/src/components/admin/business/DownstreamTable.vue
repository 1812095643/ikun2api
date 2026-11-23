<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import DataTable from '@/components/common/DataTable.vue'
import Toggle from '@/components/common/Toggle.vue'
import { type DownstreamUser } from '@/api/admin/business'
import { businessMoney } from '@/utils/business'
import { formatDateTime } from '@/utils/format'
defineProps<{
  items: DownstreamUser[]
  loading: boolean
  selected: number[]
  policyBusy?: number[]
}>()
defineEmits<{
  'update:selected': [ids: (string | number)[]]
  price: [user: DownstreamUser, group?: number]
  usage: [user: DownstreamUser]
  credit: [user: DownstreamUser]
  debt: [user: DownstreamUser]
  sort: [key: string, order: 'asc' | 'desc']
  policy: [user: DownstreamUser, enabled: boolean]
}>()
const { t } = useI18n()
const columns = computed(() =>
  [
    'user',
    'group_rates',
    'downstream_recharge_only',
    'balance',
    'outstanding',
    'requests',
    'tokens',
    'cost',
    'last_used_at',
    'actions'
  ].map((key) => ({
    key,
    sortable: !['group_rates', 'actions'].includes(key),
    label: t(
      `business.${key === 'group_rates' ? 'customRate' : key === 'last_used_at' ? 'lastUsed' : key}`
    )
  }))
)
</script>
<template>
  <DataTable
    :columns="columns"
    :data="items"
    :loading="loading"
    row-key="id"
    selectable
    :selected-keys="selected"
    :sticky-first-column="true"
    :sticky-actions-column="true"
    server-side-sort
    default-sort-key="id"
    default-sort-order="desc"
    sort-storage-key="admin-downstreams-sort"
    @sort="(key, order) => $emit('sort', key, order)"
    @update:selected-keys="$emit('update:selected', $event)"
  >
    <template #cell-user="{ row }"
      ><div class="min-w-40">
        <p class="font-medium">{{ row.username || row.email }}</p>
        <p v-if="row.username" class="mt-1 text-xs text-gray-500">
          {{ row.email }}
        </p>
      </div></template
    >
    <template #cell-group_rates="{ row }"
      ><div class="flex max-w-64 flex-wrap gap-1.5">
        <button
          v-for="rate in row.group_rates?.slice(0, 2)"
          :key="rate.group_id"
          class="badge badge-gray max-w-56 gap-1.5"
          :disabled="loading"
          @click="$emit('price', row, rate.group_id)"
        >
          <span class="max-w-32 truncate">{{ rate.group_name }}</span
          ><strong class="tabular-nums">{{ rate.rate }}×</strong></button
        ><button
          v-if="row.group_rates?.length > 2"
          class="text-xs text-gray-500"
          @click="$emit('price', row)"
        >
          +{{ row.group_rates.length - 2 }}</button
        ><span v-if="!row.group_rates?.length" class="text-xs text-gray-400">{{
          t('business.keepGroupRate')
        }}</span>
      </div></template
    >
    <template #cell-balance="{ value }"
      ><span class="tabular-nums">{{ businessMoney(value) }}</span></template
    >
    <template #cell-outstanding="{ value, row }"
      ><button
        class="tabular-nums"
        :class="
          Number(value) > 0
            ? 'font-medium text-amber-700 dark:text-amber-300'
            : 'text-gray-400'
        "
        @click="$emit('debt', row)"
      >
        {{ businessMoney(value) }}
      </button></template
    >
    <template #cell-requests="{ value }">{{ value.toLocaleString() }}</template>
    <template #cell-downstream_recharge_only="{ row }">
      <Toggle :model-value="row.downstream_recharge_only || false" :disabled="loading || policyBusy?.includes(row.id)"
        :aria-label="`${row.email} ${t('business.downstream_recharge_only')}`"
        @update:model-value="$emit('policy', row, $event)" />
    </template>
    <template #cell-tokens="{ value }">{{ value.toLocaleString() }}</template>
    <template #cell-cost="{ value }"
      ><span class="tabular-nums">{{ businessMoney(value) }}</span></template
    >
    <template #cell-last_used_at="{ value }"
      ><span class="text-xs text-gray-500">{{
        value ? formatDateTime(value) : '—'
      }}</span></template
    >
    <template #cell-actions="{ row }"
      ><div class="flex items-center gap-2">
        <button
          class="btn btn-secondary btn-sm"
          :disabled="loading"
          @click="$emit('price', row)"
        >
          {{ t('business.pricing') }}</button
        ><button
          class="btn btn-secondary btn-sm"
          :disabled="loading"
          @click="$emit('usage', row)"
        >
          {{ t('business.usage') }}</button
        ><button
          class="btn btn-secondary btn-sm"
          :disabled="loading"
          @click="$emit('credit', row)"
        >
          {{ t('business.creditAction') }}
        </button>
      </div></template
    >
    <template #empty
      ><p class="py-10 text-center text-sm text-gray-400">
        {{ t('business.emptyDownstream') }}
      </p></template
    >
  </DataTable>
</template>
