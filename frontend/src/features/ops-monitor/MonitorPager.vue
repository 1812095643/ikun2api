<script setup lang="ts">
import { computed } from 'vue'
const props = defineProps<{ total: number; size: number; label: string }>()
const page = defineModel<number>({ required: true })
const pages = computed(() => Math.max(1, Math.ceil(props.total / props.size)))
const from = computed(() => (props.total ? (page.value - 1) * props.size + 1 : 0))
const to = computed(() => Math.min(props.total, page.value * props.size))
</script>
<template>
  <nav v-if="total > size" class="mt-2 flex items-center justify-between gap-3 text-xs text-gray-500" :aria-label="`${label}分页`">
    <span class="tabular-nums">第 {{ from }}–{{ to }} 条，共 {{ total }} 条</span>
    <span class="flex items-center gap-1">
      <button type="button" class="rounded-md px-2.5 py-1 hover:bg-gray-100 disabled:opacity-40 dark:hover:bg-dark-800" :disabled="page <= 1" @click="page--">上一页</button>
      <span class="px-1 tabular-nums text-gray-700 dark:text-gray-300">{{ page }} / {{ pages }}</span>
      <button type="button" class="rounded-md px-2.5 py-1 hover:bg-gray-100 disabled:opacity-40 dark:hover:bg-dark-800" :disabled="page >= pages" @click="page++">下一页</button>
    </span>
  </nav>
</template>
