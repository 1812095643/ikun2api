<script setup lang="ts">
withDefaults(
  defineProps<{
    steps: string[]
    current: number
    label?: string
    accent?: 'primary' | 'blue'
  }>(),
  { label: '表单步骤', accent: 'primary' }
)
</script>

<template>
  <ol class="mb-6 flex items-start justify-center gap-3 sm:items-center sm:gap-4" :aria-label="label">
    <template v-for="(title, index) in steps" :key="title">
      <li
        v-if="index"
        class="mt-4 h-0.5 w-5 shrink-0 bg-gray-300 sm:mt-0 sm:w-8 dark:bg-dark-600"
        aria-hidden="true"
        role="presentation"
      />
      <li
        class="flex min-w-0 flex-col items-center gap-2 sm:flex-row"
        :aria-current="current === index + 1 ? 'step' : undefined"
      >
        <span
          class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-sm font-semibold"
          :class="current >= index + 1
            ? accent === 'blue' ? 'bg-blue-500 text-white' : 'bg-primary-500 text-white'
            : 'bg-gray-200 text-gray-500 dark:bg-dark-600'"
        >{{ index + 1 }}</span>
        <span class="text-center text-xs font-medium text-gray-700 sm:text-sm dark:text-gray-300">{{ title }}</span>
      </li>
    </template>
  </ol>
</template>
