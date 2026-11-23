<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Toggle from '@/components/common/Toggle.vue'
import BusinessError from './BusinessError.vue'
import { adminAPI } from '@/api/admin'
import { businessAPI, type DownstreamApply, type DownstreamUser } from '@/api/admin/business'
import {
  businessMoney,
  positiveDecimal,
  newBusinessOperationKey
} from '@/utils/business'
import { useBusinessRequest } from '@/composables/useBusinessRequest'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { AdminUser, Group, PaginatedResponse } from '@/types'
export type DownstreamAction = 'add' | 'price' | 'remove' | 'purchase'
const props = defineProps<{
  mode: DownstreamAction | null
  userIds: number[]
  initialGroup?: number | null
  users?: DownstreamUser[]
}>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const app = useAppStore()
const rates = reactive<Record<number, string>>({})
const originalRates = ref<Record<number, string>>({})
const mixedGroups = ref<number[]>([])
const changedGroups = ref<number[]>([])
const selected = ref<number[]>([])
const search = ref('')
const key = ref(newBusinessOperationKey())
const submitting = ref(false)
const rechargeOnly = ref(false)
const groups = ref<Group[]>([])
const groupError = ref('')
const groupsLoading = ref(false)
let groupSequence = 0
const { data, loading, error, load, clear } =
  useBusinessRequest<PaginatedResponse<AdminUser>>()
const ids = computed(() =>
  props.mode === 'add' ? selected.value : props.userIds
)
const changedRates = computed(() => Object.fromEntries(changedGroups.value
  .filter(id => rates[id] !== originalRates.value[id] || mixedGroups.value.includes(id))
  .map(id => [id, rates[id].trim() === '' ? null : Number(rates[id])])) as Record<number, number | null>)
const title = computed(() =>
  t(
    `business.${props.mode === 'add' ? 'addDownstream' : props.mode === 'price' ? 'pricing' : props.mode === 'purchase' ? 'purchasePolicy' : 'removeDownstream'}`
  )
)
const valid = computed(
  () =>
    ids.value.length > 0 &&
    (props.mode !== 'price' ||
      (!groupsLoading.value && !groupError.value && Object.keys(changedRates.value).length > 0 &&
        Object.values(changedRates.value).every(rate => rate === null ||
          (positiveDecimal(String(rate), 4) && rate <= 100000))))
)
function updateRate(id: number, value: string) {
  rates[id] = value
  if (!changedGroups.value.includes(id)) changedGroups.value.push(id)
  key.value = newBusinessOperationKey()
}
function reloadUsers() {
  void load((signal) =>
    adminAPI.users.list(
      1,
      50,
      {
        search: search.value,
        is_downstream: false,
        role: 'user',
        include_subscriptions: false
      },
      { signal }
    )
  )
}
async function reloadGroups() {
  const sequence = ++groupSequence
  groupsLoading.value = true
  groupError.value = ''
  try {
    const [allGroups, user] = await Promise.all([
      adminAPI.groups.getAllIncludingInactive(),
      props.userIds.length === 1
        ? adminAPI.users.getById(props.userIds[0])
        : Promise.resolve(null)
    ])
    if (sequence !== groupSequence || props.mode !== 'price') return
    groups.value = allGroups
    const users = props.users?.filter(item => props.userIds.includes(item.id)) || []
    if (!user && users.length !== props.userIds.length) throw new Error(t('business.loadingFailed'))
    for (const group of allGroups) {
      const values = user ? [user.group_rates?.[group.id] ?? null] : users.map(item =>
        item.group_rates.find(rate => rate.group_id === group.id)?.rate ?? null)
      const mixed = values.some(value => value !== values[0])
      if (mixed) mixedGroups.value.push(group.id)
      rates[group.id] = !mixed && values[0] != null ? String(values[0]) : ''
    }
    originalRates.value = { ...rates }
  } catch (e) {
    if (sequence === groupSequence)
      groupError.value = extractApiErrorMessage(e, t('business.loadingFailed'))
  } finally {
    if (sequence === groupSequence) groupsLoading.value = false
  }
}
watch(
  () => props.mode,
  (mode) => {
    clear()
    groupSequence++
    groups.value = []
    Object.keys(rates).forEach(id => delete rates[Number(id)])
    originalRates.value = {}
    mixedGroups.value = []
    changedGroups.value = []
    selected.value = []
    const purchaseUsers = props.users?.filter(user => props.userIds.includes(user.id)) || []
    rechargeOnly.value = purchaseUsers.length > 0 && purchaseUsers.length === props.userIds.length && purchaseUsers.every(user => user.downstream_recharge_only)
    search.value = ''
    key.value = newBusinessOperationKey()
    if (mode === 'add') reloadUsers()
    if (mode === 'price') void reloadGroups()
  },
  { immediate: true }
)
watch(
  () => [...ids.value],
  () => {
    key.value = newBusinessOperationKey()
  }
)
let timer: ReturnType<typeof setTimeout> | undefined
watch(search, () => {
  clearTimeout(timer)
  if (props.mode === 'add') timer = setTimeout(reloadUsers, 250)
})
onBeforeUnmount(() => {
  clearTimeout(timer)
  groupSequence++
})
function toggle(id: number) {
  selected.value = selected.value.includes(id)
    ? selected.value.filter((item) => item !== id)
    : [...selected.value, id]
}
async function submit() {
  if (!valid.value || submitting.value) return
  const request: DownstreamApply = {
    user_ids: [...ids.value],
    operation_key: key.value
  }
  if (props.mode === 'add') request.is_downstream = true
  if (props.mode === 'price') request.group_rates = changedRates.value
  if (props.mode === 'remove') request.is_downstream = false
  if (props.mode === 'purchase') request.downstream_recharge_only = rechargeOnly.value
  submitting.value = true
  try {
    const result = await businessAPI.apply(request)
    app.showSuccess(t('business.applied', { count: result.affected }))
    emit('saved')
    emit('close')
  } catch (e) {
    app.showError(extractApiErrorMessage(e, t('common.error')))
  } finally {
    submitting.value = false
  }
}
</script>
<template>
  <BaseDialog
    :show="!!mode"
    :title="title"
    :width="mode === 'price' ? 'wide' : 'normal'"
    :close-on-escape="!submitting"
    :show-close-button="!submitting"
    @close="!submitting && $emit('close')"
  >
    <fieldset :disabled="submitting" class="space-y-5">
      <template v-if="mode === 'add'"
        ><input
          v-model="search"
          class="input"
          :placeholder="t('business.searchUser')"
          :aria-label="t('business.searchUser')"
        /><BusinessError v-if="error" :message="error" @retry="reloadUsers" />
        <div v-else-if="loading" class="py-6 text-center text-gray-400">
          {{ t('common.loading') }}
        </div>
        <div
          v-else
          class="max-h-64 overflow-y-auto rounded-xl border border-gray-200 dark:border-dark-600"
        >
          <label
            v-for="user in data?.items"
            :key="user.id"
            class="flex cursor-pointer items-center gap-3 border-b border-gray-100 px-3 py-2.5 text-sm last:border-0 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700"
            ><input
              type="checkbox"
              :checked="selected.includes(user.id)"
              @change="toggle(user.id)"
            /><span class="min-w-0 flex-1 truncate">{{ user.email }}</span
            ><span class="shrink-0 text-xs text-gray-500">{{
              businessMoney(user.balance)
            }}</span></label
          >
          <p
            v-if="!data?.items.length"
            class="p-6 text-center text-sm text-gray-400"
          >
            {{ t('empty.noData') }}
          </p>
        </div></template
      >
      <p class="text-sm text-gray-500">
        {{ t('business.selected', { count: ids.length }) }}
      </p>
      <template v-if="mode === 'price'"
        ><BusinessError
          v-if="groupError"
          :message="groupError"
          @retry="reloadGroups"
        />
        <div v-else-if="groupsLoading" class="py-8 text-center text-sm text-gray-400">{{ t('common.loading') }}</div>
        <div v-else class="space-y-2">
          <div v-for="group in groups" :key="group.id" class="flex flex-wrap items-center gap-3 rounded-xl border p-3 sm:flex-nowrap"
            :class="group.is_exclusive ? 'border-primary-200 bg-primary-50/30 dark:border-primary-800/50 dark:bg-primary-900/10' : 'border-gray-200 dark:border-dark-600'">
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2">
                <PlatformIcon :platform="group.platform" size="sm" />
                <span class="truncate text-sm font-semibold text-gray-900 dark:text-gray-100">{{ group.name }}</span>
              </div>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('business.publicRate') }} <span class="font-medium tabular-nums text-gray-700 dark:text-gray-300">{{ group.rate_multiplier }}×</span></p>
            </div>
            <div class="flex items-center gap-2">
              <label :for="`downstream-rate-${group.id}`" class="whitespace-nowrap text-xs text-gray-500 dark:text-gray-400">{{ t('business.customRate') }}</label>
              <input :id="`downstream-rate-${group.id}`" :value="rates[group.id]" class="input hide-spinner w-28 py-2 text-sm tabular-nums" type="text" inputmode="decimal"
                :aria-label="`${group.name} ${t('business.customRate')}`"
                :placeholder="mixedGroups.includes(group.id) && !changedGroups.includes(group.id) ? t('business.mixedRate') : String(group.rate_multiplier)"
                @input="updateRate(group.id, ($event.target as HTMLInputElement).value)" />
              <button type="button" class="rounded-md px-2 py-1 text-xs text-primary-600 hover:bg-primary-50 dark:text-primary-400 dark:hover:bg-primary-900/20"
                :disabled="!rates[group.id] && !mixedGroups.includes(group.id)" @click="updateRate(group.id, '')">{{ t('common.reset') }}</button>
            </div>
          </div>
        </div>
      </template>
      <p v-if="mode === 'remove'" class="text-sm text-gray-500">
        {{ t('business.removeHint') }}
      </p>
      <div v-if="mode === 'purchase'" class="flex items-center justify-between gap-4">
        <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('business.downstream_recharge_only') }}</span>
        <Toggle v-model="rechargeOnly" :disabled="submitting" :aria-label="t('business.downstream_recharge_only')" @update:model-value="key = newBusinessOperationKey()" />
      </div>
    </fieldset>
    <template #footer
      ><div class="flex justify-end gap-2">
        <button
          class="btn btn-secondary"
          :disabled="submitting"
          @click="$emit('close')"
        >
          {{ t('common.cancel') }}</button
        ><button
          class="btn"
          :class="mode === 'remove' ? 'btn-danger' : 'btn-primary'"
          :disabled="!valid || submitting"
          @click="submit"
        >
          {{ submitting ? t('common.saving') : t('common.confirm') }}
        </button>
      </div></template
    >
  </BaseDialog>
</template>
