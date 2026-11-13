<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const enabled = defineModel<boolean>('enabled', { default: false })
const { t } = useI18n()
</script>
<template>
  <label
    class="flex cursor-pointer items-center gap-2 rounded-xl bg-gray-50 px-4 py-3 text-sm font-medium text-gray-700 dark:bg-dark-700/50 dark:text-gray-200"
  >
    <input
      v-model="enabled"
      type="checkbox"
      class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
      data-test="downstream-toggle"
    />
    {{ t('business.isDownstream') }}
  </label>
</template>
