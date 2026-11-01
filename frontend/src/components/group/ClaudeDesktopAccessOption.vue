<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'

defineProps<{ restricted: boolean }>()
const allowed = defineModel<boolean>({ required: true })
const { t } = useI18n()
</script>

<template>
  <div v-if="restricted" class="mt-3 flex items-center justify-between gap-4 rounded-lg bg-gray-50 p-3 dark:bg-dark-700/50" data-testid="claude-desktop-access">
    <div>
      <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.groups.claudeCode.allowDesktop') }}</span>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.groups.claudeCode.allowDesktopHint') }}</p>
    </div>
    <Toggle v-model="allowed" :aria-label="t('admin.groups.claudeCode.allowDesktop')" />
  </div>
</template>
