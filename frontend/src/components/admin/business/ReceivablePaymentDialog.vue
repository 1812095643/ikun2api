<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import BusinessError from './BusinessError.vue'
import {
  businessAPI,
  type ReceivableAccount,
  type ReceivableHistory
} from '@/api/admin/business'
import {
  newBusinessOperationKey,
  businessMoney,
  positiveDecimal
} from '@/utils/business'
import { useBusinessRequest } from '@/composables/useBusinessRequest'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
const props = defineProps<{ user: ReceivableAccount | null }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const app = useAppStore()
const { data, loading, error, load, clear } =
  useBusinessRequest<ReceivableHistory>()
const form = reactive({ amount: '', method: '', notes: '', paidAt: '' })
const submitting = ref(false)
const key = ref(newBusinessOperationKey())
watch(
  () => [form.amount, form.method, form.notes, form.paidAt],
  () => {
    key.value = newBusinessOperationKey()
  }
)
const outstanding = computed(() => data.value?.account.outstanding)
function reload() {
  if (props.user)
    void load((signal) => businessAPI.history(props.user!.user_id, 1, signal))
}
watch(
  () => props.user,
  (user) => {
    clear()
    Object.assign(form, { amount: '', method: '', notes: '', paidAt: '' })
    key.value = newBusinessOperationKey()
    if (user) reload()
  },
  { immediate: true }
)
async function submit() {
  if (submitting.value || loading.value || !data.value || !props.user) return
  if (!positiveDecimal(form.amount, 8)) {
    app.showError(t('business.invalidAmount'))
    return
  }
  if (Number(form.amount) > Number(outstanding.value)) {
    app.showError(t('business.excessPayment'))
    return
  }
  const userID = props.user.user_id
  const payload = {
    amount: form.amount,
    payment_method: form.method,
    notes: form.notes,
    operation_key: key.value,
    ...(form.paidAt ? { occurred_at: new Date(form.paidAt).toISOString() } : {})
  }
  submitting.value = true
  try {
    await businessAPI.payment(userID, payload)
    app.showSuccess(t('business.collected'))
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
    :show="!!user"
    :title="t('business.registerPayment')"
    width="narrow"
    :close-on-escape="!submitting"
    :show-close-button="!submitting"
    @close="!submitting && $emit('close')"
  >
    <BusinessError v-if="error" :message="error" @retry="reload" />
    <form
      v-else
      id="receivable-payment"
      class="space-y-4"
      @submit.prevent="submit"
    >
      <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-700/50">
        <p class="break-all text-sm font-medium">{{ user?.email }}</p>
        <div class="mt-3 flex items-center justify-between gap-3 text-sm">
          <span class="text-gray-500">{{ t('business.currentDebt') }}</span
          ><strong class="tabular-nums text-amber-700 dark:text-amber-300">{{
            loading ? '…' : businessMoney(outstanding)
          }}</strong>
        </div>
      </div>
      <fieldset :disabled="submitting || loading || !data" class="space-y-4">
        <div>
          <label for="receipt-amount" class="input-label">{{
            t('business.amount')
          }}</label>
          <div class="flex gap-2">
            <input
              id="receipt-amount"
              v-model="form.amount"
              type="text"
              inputmode="decimal"
              class="input"
              required
              autocomplete="off"
            /><button
              type="button"
              class="btn btn-secondary shrink-0"
              @click="form.amount = outstanding || ''"
            >
              {{ t('business.settleAll') }}
            </button>
          </div>
        </div>
        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <label for="receipt-method" class="input-label">{{
              t('business.method')
            }}</label
            ><input
              id="receipt-method"
              v-model="form.method"
              class="input"
              maxlength="100"
            />
          </div>
          <div>
            <label for="receipt-date" class="input-label">{{
              t('business.paidAt')
            }}</label
            ><input
              id="receipt-date"
              v-model="form.paidAt"
              type="datetime-local"
              class="input"
            />
          </div>
        </div>
        <div>
          <label for="receipt-notes" class="input-label">{{
            t('business.notes')
          }}</label
          ><textarea
            id="receipt-notes"
            v-model="form.notes"
            class="input"
            rows="2"
            maxlength="2000"
          />
        </div>
      </fieldset>
    </form>
    <template #footer
      ><div class="flex justify-end gap-2">
        <button
          class="btn btn-secondary"
          :disabled="submitting"
          @click="$emit('close')"
        >
          {{ t('common.cancel') }}</button
        ><button
          type="submit"
          form="receivable-payment"
          class="btn btn-primary"
          :disabled="submitting || loading || !data || !form.amount"
        >
          {{ submitting ? t('common.saving') : t('business.registerPayment') }}
        </button>
      </div></template
    >
  </BaseDialog>
</template>
