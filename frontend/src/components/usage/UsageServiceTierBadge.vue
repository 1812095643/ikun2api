<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { normalizeUsageServiceTier } from '@/utils/usageServiceTier'

const props = defineProps<{ row: { service_tier?: string | null; service_tier_multiplier?: number | null; model?: string; upstream_model?: string | null } }>()
const { t } = useI18n()
const tier = computed(() => normalizeUsageServiceTier(props.row.service_tier))
const visible = computed(() => tier.value === 'priority' || tier.value === 'ultrafast')
const label = computed(() => {
  const value = props.row.service_tier_multiplier
  // Historical rows have no price snapshot; do not invent a multiplier.
  const multiplier = value != null && Number.isFinite(value) && value > 0 ? value : undefined
  const name = tier.value === 'ultrafast' ? 'Ultrafast' : 'Fast'
  return multiplier == null ? name : `${name} ×${multiplier}`
})
</script>

<template>
  <span v-if="visible" data-testid="service-tier-billing-marker" :data-service-tier="tier" :title="t('openaiServiceTiers.billingTier')"
    class="inline-flex shrink-0 items-center gap-0.5 whitespace-nowrap rounded px-1 py-px text-[10px] font-semibold leading-tight ring-1 ring-inset"
    :class="tier === 'ultrafast'
      ? 'ultrafast-billing-marker bg-fuchsia-50 text-fuchsia-700 ring-fuchsia-300 dark:bg-fuchsia-500/15 dark:text-fuchsia-200 dark:ring-fuchsia-400/40'
      : 'bg-sky-50 text-sky-700 ring-sky-200 dark:bg-sky-500/15 dark:text-sky-300 dark:ring-sky-500/30'">
    <svg v-if="tier === 'ultrafast'" class="h-2.5 w-2.5" viewBox="0 0 12 12" fill="currentColor" aria-hidden="true"><path d="M7.5.5 2 7h3L4.5 11.5 10 5H7L7.5.5Z" /></svg>{{ label }}
  </span>
</template>

<style scoped>
.ultrafast-billing-marker { box-shadow: inset 0 1px 0 rgb(255 255 255 / .3), 0 0 5px rgb(192 38 211 / .12); }
</style>
