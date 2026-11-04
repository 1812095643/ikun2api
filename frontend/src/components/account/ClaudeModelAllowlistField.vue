<script setup lang="ts">
import { computed } from 'vue'
import ModelWhitelistSelector from './ModelWhitelistSelector.vue'
export interface AccountModelAllowlist { enabled: boolean; models: string[] }
const props = defineProps<{ modelValue: AccountModelAllowlist; accountId?: number }>()
const emit = defineEmits<{ 'update:modelValue': [value: AccountModelAllowlist] }>()
const enabled = computed({ get: () => props.modelValue.enabled, set: value => emit('update:modelValue', { ...props.modelValue, enabled: value }) })
const models = computed({ get: () => props.modelValue.models, set: value => emit('update:modelValue', { ...props.modelValue, models: value }) })
</script>
<template>
  <section class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-600" data-testid="claude-account-allowlist">
    <label class="flex items-center justify-between gap-3 text-sm font-medium text-gray-900 dark:text-gray-100">
      Claude 账号模型白名单
      <input v-model="enabled" type="checkbox" class="rounded border-gray-300 text-primary-600" />
    </label>
    <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">开启后，仅使用此账号处理选中的模型。分组白名单同时生效；模型映射和 OAuth 请求协议保持原有设置。</p>
    <template v-if="enabled">
      <ModelWhitelistSelector v-model="models" platform="anthropic" :account-id="accountId" />
      <p v-if="!models.length" role="alert" class="text-xs text-amber-600 dark:text-amber-400">请至少选择一个模型后保存。</p>
    </template>
  </section>
</template>
