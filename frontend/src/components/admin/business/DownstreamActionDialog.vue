<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import BusinessError from './BusinessError.vue'
import { adminAPI } from '@/api/admin'
import { businessAPI, type DownstreamApply } from '@/api/admin/business'
import {
  businessMoney,
  positiveDecimal,
  newBusinessOperationKey
} from '@/utils/business'
import { useBusinessRequest } from '@/composables/useBusinessRequest'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { AdminUser, Group, PaginatedResponse } from '@/types'
export type DownstreamAction = 'add' | 'price' | 'remove'
const props = defineProps<{
  mode: DownstreamAction | null
  userIds: number[]
  initialGroup?: number | null
}>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const app = useAppStore()
const form = reactive({
  group: null as number | null,
  rate: '',
  restore: false
})
const selected = ref<number[]>([])
const search = ref('')
const key = ref(newBusinessOperationKey())
const submitting = ref(false)
const groups = ref<Group[]>([])
const groupError = ref('')
const groupsLoading = ref(false)
const currentRates = ref<Record<number, number>>({})
let groupSequence = 0
const { data, loading, error, load, clear } =
  useBusinessRequest<PaginatedResponse<AdminUser>>()
const ids = computed(() =>
  props.mode === 'add' ? selected.value : props.userIds
)
const groupOptions = computed(() =>
  groups.value.map((group) => ({
    value: group.id,
    label: `${group.name} · ${group.rate_multiplier}×`
  }))
)
const title = computed(() =>
  t(
    `business.${props.mode === 'add' ? 'addDownstream' : props.mode === 'price' ? 'pricing' : 'removeDownstream'}`
  )
)
const valid = computed(
  () =>
    ids.value.length > 0 &&
    (props.mode !== 'price' ||
      (form.group &&
        !groupsLoading.value &&
        !groupError.value &&
        (form.restore ||
          (positiveDecimal(form.rate, 4) && Number(form.rate) <= 100000))))
)
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
    currentRates.value = user?.group_rates || {}
    if (form.group)
      form.rate =
        currentRates.value[form.group] != null
          ? String(currentRates.value[form.group])
          : ''
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
    currentRates.value = {}
    selected.value = []
    search.value = ''
    Object.assign(form, {
      group: props.initialGroup || null,
      rate: '',
      restore: false
    })
    key.value = newBusinessOperationKey()
    if (mode === 'add') reloadUsers()
    if (mode === 'price') void reloadGroups()
  }
)
watch(
  () => form.group,
  (group) => {
    form.rate =
      group && currentRates.value[group] != null
        ? String(currentRates.value[group])
        : ''
  }
)
watch(
  () => [form.group, form.rate, form.restore, ...ids.value],
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
  if (props.mode === 'price' && form.group)
    request.group_rates = {
      [form.group]: form.restore ? null : Number(form.rate)
    }
  if (props.mode === 'remove') request.is_downstream = false
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
    width="normal"
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
        <div>
          <label class="input-label">{{ t('business.group') }}</label
          ><Select
            v-model="form.group"
            :aria-label="t('business.group')"
            :options="groupOptions"
            :disabled="groupsLoading"
            :placeholder="t('business.chooseGroup')"
          />
        </div>
        <div>
          <label for="downstream-rate" class="input-label">{{
            t('business.customRate')
          }}</label
          ><input
            id="downstream-rate"
            v-model="form.rate"
            type="text"
            inputmode="decimal"
            class="input"
            :disabled="form.restore || groupsLoading"
            :placeholder="t('business.keepGroupRate')"
          />
        </div>
        <label class="flex cursor-pointer items-center gap-2 text-sm"
          ><input v-model="form.restore" type="checkbox" />{{
            t('business.clearRate')
          }}</label
        ></template
      >
      <p v-if="mode === 'remove'" class="text-sm text-gray-500">
        {{ t('business.removeHint') }}
      </p>
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
