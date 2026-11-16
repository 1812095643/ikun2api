<script setup lang="ts">
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'

export type UsageUserAction = 'usage' | 'balance' | 'notify'
defineProps<{ email: string }>()
const emit = defineEmits<{ select: [action: UsageUserAction] }>()
const options: { value: UsageUserAction; label: string; icon: 'chart' | 'creditCard' | 'bell' }[] = [
  { value: 'usage', label: '查看使用记录', icon: 'chart' },
  { value: 'balance', label: '查看充值记录', icon: 'creditCard' },
  { value: 'notify', label: '通知用户', icon: 'bell' }
]
function select(value: string | number | boolean | null) {
  if (value === 'usage' || value === 'balance' || value === 'notify') emit('select', value)
}
</script>

<template>
  <Select class="usage-user-menu" :model-value="null" :options="options" :searchable="false"
    :aria-label="`${email} 用户操作`" @update:model-value="select">
    <template #selected><span class="truncate">{{ email }}</span></template>
    <template #option="{ option }">
      <span class="flex items-center gap-2"><Icon :name="options.find(item => item.value === option.value)?.icon || 'user'" size="sm" />{{ option.label }}</span>
    </template>
  </Select>
</template>

<style scoped>
.usage-user-menu { @apply inline-block max-w-full align-middle; }
.usage-user-menu :deep(.select-trigger) { @apply min-h-0 gap-1 rounded px-0 py-0 text-sm font-medium text-primary-600 shadow-none dark:text-primary-400; border-color: transparent; background: transparent; }
.usage-user-menu :deep(.select-value) { @apply underline decoration-dashed underline-offset-2; }
.usage-user-menu :deep(.select-icon) { @apply w-3 shrink-0 opacity-60; }
</style>
