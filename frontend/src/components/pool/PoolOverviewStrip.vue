<script setup lang="ts">
defineProps<{ items: { key: string; label: string; value: string; hint: string }[]; loading?: boolean; unavailable?: boolean }>()
</script>

<template>
  <dl class="grid grid-cols-2 gap-x-5 gap-y-3 rounded-xl border border-gray-200 bg-white px-4 py-3 dark:border-dark-700 dark:bg-dark-800 sm:grid-cols-3" :class="items.length > 4 ? 'xl:grid-cols-6' : 'xl:grid-cols-4'" :aria-busy="loading">
    <div v-for="item in items" :key="item.key" class="min-w-0">
      <dt class="truncate text-xs text-gray-500 dark:text-dark-400">{{ item.label }}</dt>
      <dd class="mt-1 flex items-baseline gap-2">
        <span v-if="loading && unavailable" class="my-1 h-5 w-16 animate-pulse rounded bg-gray-100 dark:bg-dark-700" />
        <span v-else class="truncate text-lg font-semibold tabular-nums text-gray-900 dark:text-white" :title="item.value">{{ unavailable ? '—' : item.value }}</span>
      </dd>
      <dd v-if="!unavailable && item.hint" class="mt-0.5 truncate text-xs text-gray-500 dark:text-dark-400" :title="item.hint">{{ item.hint }}</dd>
    </div>
  </dl>
</template>
