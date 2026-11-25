<script setup lang="ts">
defineProps<{ label: string; disabled?: boolean }>()
const on = defineModel<boolean>({ required: true })
</script>
<template>
  <button type="button" role="switch" :aria-checked="on" :aria-label="label" :disabled="disabled" class="relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-400 focus-visible:ring-offset-2 disabled:opacity-40 dark:focus-visible:ring-offset-dark-900" :class="on ? 'bg-primary-600' : 'bg-gray-300 dark:bg-dark-600'" @click="on = !on">
    <span class="inline-block h-4 w-4 rounded-full bg-white shadow transition-transform motion-reduce:transition-none" :class="on ? 'translate-x-[18px]' : 'translate-x-0.5'" />
  </button>
</template>
