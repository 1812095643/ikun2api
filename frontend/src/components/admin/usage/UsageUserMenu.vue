<script setup lang="ts">
import UserEmailMenu from '@/components/admin/user/UserEmailMenu.vue'

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
  <UserEmailMenu :email="email" :options="options" @select="select" />
</template>
