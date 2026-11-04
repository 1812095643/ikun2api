<script setup lang="ts">
import { computed } from 'vue'
import { subscriptionMoney as money, subscriptionTime as time } from '@/api/universalSubscriptions'
const props = defineProps<{ label: string; used: number; limit: number | null; resetAt?: string; expiresAt?: string }>()
const percent = computed(() => props.limit ? Math.min(100, Math.max(0, props.used / props.limit * 100)) : 0)
</script>
<template>
 <div class="space-y-1.5"><div class="flex items-center justify-between gap-3 text-xs"><span class="text-gray-500 dark:text-gray-400">{{ label }}</span><span class="tabular-nums text-gray-800 dark:text-gray-200">{{ money(used) }} / {{ money(limit) }}</span></div><div v-if="limit" class="h-1.5 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700" role="progressbar" :aria-label="label" :aria-valuenow="Math.round(percent)" :aria-valuemin="0" :aria-valuemax="100"><div class="h-full rounded-full" :class="percent >= 100 ? 'bg-amber-500' : 'bg-primary-500'" :style="{ width: `${percent}%` }" /></div><p v-if="resetAt && limit" class="text-[11px] text-gray-400">{{ time(resetAt) }} {{ expiresAt && new Date(resetAt).getTime() >= new Date(expiresAt).getTime() ? '本期结束' : '重置' }}</p></div>
</template>
