<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import { adminAPI } from '@/api/admin'
import { businessAPI } from '@/api/admin/business'
import type { AdminUser, PaginatedResponse } from '@/types'
import { useBusinessRequest } from '@/composables/useBusinessRequest'
import { useAppStore } from '@/stores/app'
import { newBusinessOperationKey, positiveDecimal } from '@/utils/business'
import { extractApiErrorMessage } from '@/utils/apiError'

const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const app = useAppStore()
const form = reactive({ amount: '', notes: '', occurredAt: '' })
const user = ref<AdminUser | null>(null)
const search = ref('')
const submitting = ref(false)
const operationKey = ref(newBusinessOperationKey())
const { data, loading, error, load } = useBusinessRequest<PaginatedResponse<AdminUser>>()
const options = computed(() => [...new Map([
  ...(user.value ? [user.value] : []), ...(data.value?.items || [])
].map(item => [item.id, item])).values()].map(item => ({ value: item.id, label: item.email })))
const valid = computed(() => !!user.value && positiveDecimal(form.amount, 8))
const selectUser = (id: string | number | boolean | null) => {
  user.value = data.value?.items.find(item => item.id === id) || (user.value?.id === id ? user.value : null)
}
function reload() {
  void load(signal => adminAPI.users.list(1, 30, { search: search.value, include_subscriptions: false }, { signal }))
}
let timer: ReturnType<typeof setTimeout> | undefined
watch(search, () => { clearTimeout(timer); timer = setTimeout(reload, 250) })
watch(() => [user.value?.id, form.amount, form.notes, form.occurredAt], () => {
  operationKey.value = newBusinessOperationKey()
}, { flush: 'sync' })
onBeforeUnmount(() => clearTimeout(timer))
reload()
async function submit() {
  if (!user.value || !valid.value || submitting.value) return
  const payload = {
    amount: form.amount, notes: form.notes, operation_key: operationKey.value,
    ...(form.occurredAt ? { occurred_at: new Date(form.occurredAt).toISOString() } : {})
  }
  submitting.value = true
  try {
    await businessAPI.credit(user.value.id, payload)
    app.showSuccess(t('business.creditSaved'))
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
  <BaseDialog :show="true" :title="t('business.addCredit')" width="normal"
    :close-on-escape="!submitting" :show-close-button="!submitting" @close="!submitting && emit('close')">
    <form id="receivable-credit" class="space-y-4" @submit.prevent="submit">
      <fieldset :disabled="submitting" class="space-y-4">
        <div>
          <label for="receivable-credit-user" class="input-label">{{ t('business.user') }}</label>
          <Select id="receivable-credit-user" :model-value="user?.id" :options="options" remote
            :loading="loading" :disabled="submitting" :placeholder="t('business.chooseUsers')"
            :search-placeholder="t('business.searchUser')" :aria-label="t('business.user')"
            @search="search = $event" @update:model-value="selectUser" />
          <p v-if="error" class="input-error-text" role="alert">{{ error }}</p>
        </div>
        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <label for="receivable-credit-amount" class="input-label">{{ t('business.amount') }}</label>
            <input id="receivable-credit-amount" v-model="form.amount" class="input" inputmode="decimal" required autocomplete="off" />
          </div>
          <div>
            <label for="receivable-credit-date" class="input-label">{{ t('business.occurredAt') }}</label>
            <input id="receivable-credit-date" v-model="form.occurredAt" class="input" type="datetime-local" />
          </div>
        </div>
        <div>
          <label for="receivable-credit-notes" class="input-label">{{ t('business.notes') }}</label>
          <textarea id="receivable-credit-notes" v-model="form.notes" class="input" rows="2" maxlength="2000" />
        </div>
      </fieldset>
    </form>
    <template #footer>
      <div class="flex w-full flex-wrap items-center justify-between gap-3">
        <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('business.manualCreditHint') }}</span>
        <div class="ml-auto flex gap-2">
          <button class="btn btn-secondary" :disabled="submitting" @click="emit('close')">{{ t('common.cancel') }}</button>
          <button type="submit" form="receivable-credit" class="btn btn-primary" :disabled="submitting || !valid">
            {{ submitting ? t('common.saving') : t('business.addCredit') }}
          </button>
        </div>
      </div>
    </template>
  </BaseDialog>
</template>
