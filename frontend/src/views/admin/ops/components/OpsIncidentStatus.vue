<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { MonitorIncident } from '@/api/admin/opsMonitor'
import { incidentSilenced, incidentStatus } from '../utils/incidentPresentation'

defineProps<{ item: MonitorIncident }>()
const { t } = useI18n()
</script>

<template>
  <div class="space-y-0.5 text-xs">
    <span :class="item.status === 'resolved' ? 'text-emerald-600' : 'text-gray-600 dark:text-gray-300'">{{ t(`admin.ops.enhanced.status.${incidentStatus(item)}`) }}</span>
    <div v-if="item.acknowledged || incidentSilenced(item)" class="flex flex-wrap gap-x-2 text-gray-400">
      <span v-if="item.acknowledged">{{ t('admin.ops.enhanced.status.acknowledged') }}</span>
      <span v-if="incidentSilenced(item)">{{ t('admin.ops.enhanced.status.silenced') }}</span>
    </div>
  </div>
</template>
