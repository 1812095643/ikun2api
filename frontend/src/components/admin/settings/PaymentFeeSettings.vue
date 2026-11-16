<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import alipayIcon from '@/assets/icons/alipay.svg'
import wxpayIcon from '@/assets/icons/wxpay.svg'

const props = defineProps<{ modelValue?: Record<string, number>; defaultRate: number }>()
const emit = defineEmits<{
  'update:modelValue': [value: Record<string, number>]
  'update:defaultRate': [value: number]
}>()
const { t } = useI18n()
const methods = [
  { key: 'alipay', label: 'payment.methods.alipay', icon: alipayIcon },
  { key: 'wxpay', label: 'payment.methods.wxpay', icon: wxpayIcon }
]
function update(value: string, method?: string) {
  const rate = Math.min(100, Math.max(0, Math.round((Number(value) || 0) * 100) / 100))
  if (!method) {
    emit('update:defaultRate', rate)
    return
  }
  const next = { ...props.modelValue }
  if (!value.trim()) delete next[method]
  else next[method] = rate
  emit('update:modelValue', next)
}
</script>

<template>
  <div data-testid="payment-fee-settings">
    <div class="grid gap-3 sm:grid-cols-3">
      <div>
        <label for="payment-fee-default" class="input-label">{{ t('admin.settings.payment.defaultFeeRate') }}</label>
        <div class="relative"><input id="payment-fee-default" :value="defaultRate" class="input pr-8" type="number" min="0" max="100" step="0.01" @input="update(($event.target as HTMLInputElement).value)" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-gray-400">%</span></div>
      </div>
      <div v-for="method in methods" :key="method.key">
        <label :for="`payment-fee-${method.key}`" class="input-label flex items-center gap-1.5"><img :src="method.icon" class="h-4 w-4" alt="" />{{ t(method.label) }}</label>
        <div class="relative"><input :id="`payment-fee-${method.key}`" :value="modelValue?.[method.key] ?? ''" class="input pr-8" type="number" min="0" max="100" step="0.01" :placeholder="String(defaultRate)" @input="update(($event.target as HTMLInputElement).value, method.key)" /><span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-gray-400">%</span></div>
      </div>
    </div>
    <p class="mt-1 text-xs text-gray-400">{{ t('admin.settings.payment.methodFeeHint') }}</p>
  </div>
</template>
