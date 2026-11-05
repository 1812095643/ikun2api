<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { formatTime } from '@/utils/format'
defineProps<{ updatedAt?: number | null; busy?: boolean }>()
defineEmits<{ retry: [] }>()
const { t } = useI18n()
</script>

<template>
  <div role="alert" class="flex flex-wrap items-center justify-between gap-3 rounded-xl bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">
    <div class="flex min-w-0 items-start gap-2">
      <Icon name="exclamationCircle" size="md" class="mt-0.5 shrink-0" />
      <div>
        <span class="font-medium">{{ t('admin.pool.networkError') }}</span>
        <p class="mt-0.5 text-xs opacity-80">{{ updatedAt ? t('admin.pool.lastAvailable', { time: formatTime(new Date(updatedAt).toISOString()) }) : t('admin.pool.retryHint') }}</p>
      </div>
    </div>
    <button type="button" class="rounded-lg px-3 py-1.5 font-medium underline underline-offset-4 hover:bg-red-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 dark:hover:bg-red-900/30" :disabled="busy" @click="$emit('retry')">{{ t('admin.pool.retry') }}</button>
  </div>
</template>
