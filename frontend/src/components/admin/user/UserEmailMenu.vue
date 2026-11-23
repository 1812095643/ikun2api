<script setup lang="ts">
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'

export interface UserEmailMenuOption extends SelectOption {
  value: string
  label: string
  icon: InstanceType<typeof Icon>['$props']['name']
}

const props = defineProps<{ email: string; options: UserEmailMenuOption[] }>()
const emit = defineEmits<{ select: [action: string] }>()

function select(value: string | number | boolean | null) {
  if (typeof value === 'string' && props.options.some(option => option.value === value)) emit('select', value)
}
</script>

<template>
  <Select class="user-email-menu" :model-value="null" :options="options" :searchable="false"
    :aria-label="`${email} 用户操作`" @update:model-value="select">
    <template #selected><span class="truncate">{{ email }}</span></template>
    <template #option="{ option }">
      <span class="flex items-center gap-2"><Icon :name="options.find(item => item.value === option.value)?.icon || 'user'" size="sm" />{{ option.label }}</span>
    </template>
  </Select>
</template>

<style scoped>
.user-email-menu { @apply inline-block max-w-full align-middle; }
.user-email-menu :deep(.select-trigger) { @apply min-h-0 gap-1 rounded px-0 py-0 text-sm font-medium text-primary-600 shadow-none dark:text-primary-400; border-color: transparent; background: transparent; }
.user-email-menu :deep(.select-value) { @apply underline decoration-dashed underline-offset-2; }
.user-email-menu :deep(.select-icon) { @apply w-3 shrink-0 opacity-60; }
</style>
