<script setup lang="ts">
import { watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import BusinessError from './BusinessError.vue'
import {
  businessAPI,
  type DownstreamUser,
  type DownstreamUsage
} from '@/api/admin/business'
import { businessMoney } from '@/utils/business'
import { useBusinessRequest } from '@/composables/useBusinessRequest'
const props = defineProps<{
  user: DownstreamUser | null
  range: Record<string, unknown>
}>()
defineEmits<{ close: [] }>()
const { t } = useI18n()
const { data, loading, error, load, clear } = useBusinessRequest<{
  items: DownstreamUsage[]
}>()
function reload() {
  if (props.user)
    void load((signal) =>
      businessAPI.usage(props.user!.id, props.range, signal)
    )
}
watch(
  () => props.user,
  (user) => {
    clear()
    if (user) reload()
  }
)
</script>
<template>
  <BaseDialog
    :show="!!user"
    :title="`${t('business.usage')} · ${user?.email || ''}`"
    width="extra-wide"
    @close="$emit('close')"
    ><BusinessError v-if="error" :message="error" @retry="reload" />
    <div v-else-if="loading" class="py-12 text-center text-gray-400">
      {{ t('common.loading') }}
    </div>
    <div v-else class="overflow-x-auto">
      <table class="w-full text-sm">
        <thead>
          <tr
            class="border-b border-gray-200 text-left text-xs text-gray-500 dark:border-dark-600"
          >
            <th
              v-for="key in ['group', 'model', 'requests', 'tokens', 'cost']"
              :key="key"
              class="px-3 py-3"
            >
              {{ t(`business.${key}`) }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="row in data?.items"
            :key="`${row.group_id}-${row.model}`"
            class="border-b border-gray-100 dark:border-dark-700"
          >
            <td class="px-3 py-3">{{ row.group_name }}</td>
            <td class="px-3 py-3">{{ row.model }}</td>
            <td class="px-3 py-3 tabular-nums">
              {{ row.requests.toLocaleString() }}
            </td>
            <td class="px-3 py-3 tabular-nums">
              {{ row.tokens.toLocaleString() }}
            </td>
            <td class="px-3 py-3 tabular-nums">
              {{ businessMoney(row.cost) }}
            </td>
          </tr>
        </tbody>
      </table>
      <p v-if="!data?.items.length" class="py-12 text-center text-gray-400">
        {{ t('business.noUsage') }}
      </p>
    </div>
    <template #footer
      ><router-link
        :to="{ path: '/admin/usage', query: { user_id: user?.id } }"
        class="btn btn-secondary"
        >{{ t('business.usageDetails') }}</router-link
      ></template
    ></BaseDialog
  >
</template>
