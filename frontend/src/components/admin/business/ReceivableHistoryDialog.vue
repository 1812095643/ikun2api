<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import BusinessError from './BusinessError.vue'
import {
  businessAPI,
  type ReceivableAccount,
  type ReceivableHistory
} from '@/api/admin/business'
import { businessMoney } from '@/utils/business'
import { useBusinessRequest } from '@/composables/useBusinessRequest'
import { formatDateTime } from '@/utils/format'
const props = defineProps<{ user: ReceivableAccount | null }>()
defineEmits<{ close: [] }>()
const { t } = useI18n()
const page = ref(1)
const { data, loading, error, load, clear } =
  useBusinessRequest<ReceivableHistory>()
function reload() {
  if (props.user)
    void load((signal) =>
      businessAPI.history(props.user!.user_id, page.value, signal)
    )
}
watch(
  () => props.user,
  (user) => {
    clear()
    page.value = 1
    if (user) reload()
  },
  { immediate: true }
)
watch(page, reload)
</script>
<template>
  <BaseDialog
    :show="!!user"
    :title="`${t('business.statement')} · ${user?.email || ''}`"
    width="extra-wide"
    @close="$emit('close')"
  >
    <BusinessError v-if="error" :message="error" @retry="reload" />
    <div v-else-if="loading" class="py-12 text-center text-gray-400">
      {{ t('common.loading') }}
    </div>
    <div v-else-if="data" class="space-y-5">
      <dl class="flex flex-wrap gap-x-10 gap-y-3 text-sm">
        <div
          v-for="key in ['outstanding', 'credited', 'received'] as const"
          :key="key"
        >
          <dt class="text-xs text-gray-500">{{ t(`business.${key}`) }}</dt>
          <dd class="mt-1 font-semibold tabular-nums">
            {{ businessMoney(data.account[key]) }}
          </dd>
        </div>
      </dl>
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr
              class="border-b border-gray-200 text-left text-xs text-gray-500 dark:border-dark-600"
            >
              <th
                v-for="key in [
                  'date',
                  'mark',
                  'amount',
                  'balanceAfter',
                  'method',
                  'operator',
                  'notes'
                ]"
                :key="key"
                class="whitespace-nowrap px-3 py-3"
              >
                {{ t(`business.${key}`) }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="entry in data.items"
              :key="entry.id"
              class="border-b border-gray-100 dark:border-dark-700"
            >
              <td class="whitespace-nowrap px-3 py-3 text-xs">
                {{ formatDateTime(entry.occurred_at) }}
              </td>
              <td class="whitespace-nowrap px-3 py-3">
                <span
                  class="badge"
                  :class="
                    entry.kind === 'credit' ? 'badge-warning' : 'badge-success'
                  "
                  >{{ t(`business.${entry.kind}`) }}</span
                >
              </td>
              <td class="px-3 py-3 tabular-nums">
                {{ entry.kind === 'payment' ? '−' : '+'
                }}{{ businessMoney(entry.amount) }}
              </td>
              <td class="px-3 py-3 tabular-nums">
                {{ businessMoney(entry.outstanding_after) }}
              </td>
              <td class="px-3 py-3">{{ entry.payment_method || '—' }}</td>
              <td class="px-3 py-3">{{ entry.actor_name }}</td>
              <td
                class="min-w-40 max-w-sm whitespace-pre-wrap break-words px-3 py-3 text-gray-500"
              >
                {{ entry.notes || '—' }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <Pagination
        v-if="data.total > 30"
        :page="page"
        :page-size="30"
        :total="data.total"
        :show-page-size-selector="false"
        @update:page="page = $event"
      />
    </div>
  </BaseDialog>
</template>
