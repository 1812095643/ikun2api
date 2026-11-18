<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'

const enabled = defineModel<boolean>({ required: true })
const { t } = useI18n()
</script>

<template>
  <div class="flex items-center justify-between gap-3">
    <span class="text-sm font-medium text-gray-700 dark:text-gray-300">
      {{ t('admin.groups.claudeCode.bridgeRequired') }}
    </span>
    <Toggle v-model="enabled" :aria-label="t('admin.groups.claudeCode.bridgeRequired')" />
  </div>
</template>
