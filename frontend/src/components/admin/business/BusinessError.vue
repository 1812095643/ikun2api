<script setup lang="ts">
import { useI18n } from 'vue-i18n'
defineProps<{ message: string }>()
defineEmits<{ retry: [] }>()
const { t } = useI18n()
</script>
<template>
  <div
    role="alert"
    class="flex items-center justify-between gap-4 rounded-xl bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300"
  >
    <span>{{ message }}</span
    ><button
      type="button"
      class="btn btn-secondary btn-sm"
      @click="$emit('retry')"
    >
      {{ t('business.refresh') }}
    </button>
  </div>
</template>
