<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'

defineProps<{ platform: string }>()
const enabled = defineModel<boolean>({ required: true })
const { t } = useI18n()
</script>

<template>
  <div v-if="platform === 'anthropic'" class="flex items-center justify-between gap-4 border-t border-gray-200 pt-4 dark:border-dark-400" data-testid="claude-weekly-reset-priority">
    <div>
      <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.groups.weeklyResetPriority.title') }}</span>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.groups.weeklyResetPriority.hint') }}</p>
    </div>
    <Toggle v-model="enabled" :aria-label="t('admin.groups.weeklyResetPriority.title')" />
  </div>
</template>
