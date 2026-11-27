<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MonitorHealth } from '@/api/admin/opsMonitor'

const props = defineProps<{ health: MonitorHealth | null; error?: string }>()
const { t } = useI18n()
const sources = computed(() => (props.health?.sources ?? []).filter(source => !['fresh', 'historical'].includes(source.quality)))
const loops = computed(() => Object.entries(props.health?.health.loops ?? {}).filter(([, loop]) => loop.failed || !loop.last_success || Date.parse(loop.last_success) < 0))
</script>

<template>
  <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs" aria-live="polite">
    <span v-if="error" class="text-amber-600">{{ t('admin.ops.enhanced.healthUnavailable') }}</span>
    <template v-else-if="health">
      <span :class="health.complete ? 'text-emerald-600' : 'text-amber-600'">{{ t(health.complete ? 'admin.ops.enhanced.complete' : 'admin.ops.enhanced.incomplete') }}</span>
      <span v-for="source in sources" :key="source.source_id" class="text-amber-600">{{ source.source_id }} · {{ source.quality }}</span>
      <span v-if="!health.health.ready" class="text-amber-600">{{ t('admin.ops.enhanced.evaluationAttention') }}<template v-for="[name] in loops" :key="name"> · {{ name }}</template></span>
      <span class="text-gray-500">{{ t('admin.ops.enhanced.pendingCount', { n: health.queue.pending || 0 }) }}</span>
      <span v-if="health.queue.uncertain" class="text-amber-600">{{ t('admin.ops.enhanced.uncertainCount', { n: health.queue.uncertain }) }}</span>
      <span v-if="health.queue.needs_attention" class="text-red-600">{{ t('admin.ops.enhanced.attentionCount', { n: health.queue.needs_attention }) }}</span>
    </template>
  </div>
</template>
