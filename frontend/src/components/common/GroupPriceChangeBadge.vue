<script setup lang="ts">
import { computed } from 'vue'
import { useNow } from '@vueuse/core'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  rateMultiplier?: number
  previousRateMultiplier?: number | null
  rateMultiplierChangedAt?: string | null
  createdAt?: string | null
  userRateMultiplier?: number | null
}>()
const { t, locale } = useI18n()
const now = useNow({ interval: 60_000 })
const change = computed(() => {
  // A fixed personal rate does not follow changes to the public base rate.
  if (props.userRateMultiplier != null || props.previousRateMultiplier == null || !props.rateMultiplierChangedAt) return null
  const current = props.rateMultiplier
  const previous = props.previousRateMultiplier
  const changedAt = Date.parse(props.rateMultiplierChangedAt)
  if (typeof current !== 'number' || !Number.isFinite(current) || !Number.isFinite(previous) || current < 0 || previous < 0 || !Number.isFinite(changedAt) || changedAt > now.value.getTime()) return null
  const delta = Math.round((current - previous) * 10_000) / 10_000
  if (!delta) return null
  return { delta, amount: String(Math.abs(delta)), previous, current, changedAt }
})
const isNew = computed(() => {
  const created = Date.parse(props.createdAt ?? '')
  const age = now.value.getTime() - created
  return Number.isFinite(created) && age >= 0 && age < 7 * 24 * 60 * 60 * 1000
})
const formatDate = (date: number) => new Date(date).toLocaleString(locale.value)
const changeTitle = computed(() => change.value
  ? t('common.groupPriceChange', { previous: change.value.previous, current: change.value.current, time: formatDate(change.value.changedAt) })
  : '')
</script>

<template>
  <span v-if="change || isNew" class="inline-flex shrink-0 items-center gap-1 whitespace-nowrap">
    <span
      v-if="change"
      class="inline-flex items-center gap-0.5 rounded px-1 py-0.5 text-[10px] font-semibold leading-none tabular-nums"
      :class="change.delta > 0
        ? 'bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300'
        : 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-300'"
      :title="changeTitle"
      :aria-label="changeTitle"
      data-price-change
    >
      <span aria-hidden="true">{{ change.delta > 0 ? '↑' : '↓' }}</span>
      {{ change.amount }}
    </span>
    <span
      v-if="isNew"
      class="rounded bg-primary-50 px-1 py-0.5 text-[9px] font-semibold leading-none text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
      :title="t('common.newGroup')"
      :aria-label="t('common.newGroup')"
      data-new-group
    >NEW</span>
  </span>
</template>
