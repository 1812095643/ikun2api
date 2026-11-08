<template>
  <div>
    <div
      v-if="loading && items.length === 0"
      class="grid gap-5 grid-cols-1 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4"
    >
      <div
        v-for="i in 6"
        :key="i"
        class="p-5 rounded-2xl min-h-[280px] bg-white/70 dark:bg-dark-800/60 border border-gray-200/80 dark:border-dark-700/70 animate-pulse"
      >
        <div class="flex items-start gap-3">
          <div class="w-9 h-9 rounded-xl bg-gray-200 dark:bg-dark-700"></div>
          <div class="flex-1 space-y-2">
            <div class="h-4 w-2/3 rounded bg-gray-200 dark:bg-dark-700"></div>
            <div class="h-3 w-1/2 rounded bg-gray-200 dark:bg-dark-700"></div>
          </div>
          <div class="h-6 w-16 rounded-full bg-gray-200 dark:bg-dark-700"></div>
        </div>
        <div class="mt-5 grid grid-cols-2 gap-2">
          <div class="h-16 rounded-xl bg-gray-100 dark:bg-dark-900/40"></div>
          <div class="h-16 rounded-xl bg-gray-100 dark:bg-dark-900/40"></div>
        </div>
        <div class="mt-6 h-5 w-full rounded bg-gray-100 dark:bg-dark-900/40"></div>
      </div>
    </div>

    <EmptyState
      v-else-if="items.length === 0"
      :title="t('channelStatus.empty.title')"
      :description="t('channelStatus.empty.description')"
    />

    <div v-else class="space-y-7">
      <section v-for="section in sections" :key="section.provider" class="space-y-3">
        <h2 class="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">{{ providerLabel(section.provider) }}<span class="rounded-md bg-gray-100 px-1.5 py-0.5 text-[10px] dark:bg-dark-700">{{ section.items.length }}</span></h2>
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
          <MonitorCard v-for="item in section.items" :key="item.id" :item="item" :window="window" :availability-value="resolveAvailability(item)" :countdown-seconds="countdownSeconds" @click="emit('cardClick', item)" />
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import { useI18n } from 'vue-i18n'
import type { UserMonitorView, UserMonitorDetail } from '@/api/channelMonitor'
import EmptyState from '@/components/common/EmptyState.vue'
import MonitorCard from './MonitorCard.vue'

const props = defineProps<{
  items: UserMonitorView[]
  window: '7d' | '15d' | '30d'
  countdownSeconds: number
  loading: boolean
  detailCache: Record<number, UserMonitorDetail>
}>()

const emit = defineEmits<{
  (e: 'cardClick', item: UserMonitorView): void
}>()

const { t } = useI18n()
const { providerLabel } = useChannelMonitorFormat()
const sections = computed(() => {
  const grouped = new Map<string, UserMonitorView[]>()
  for (const item of props.items) {
    const rows = grouped.get(item.provider) ?? []
    rows.push(item)
    grouped.set(item.provider, rows)
  }
  return [...grouped].map(([provider, items]) => ({ provider, items }))
})

function resolveAvailability(item: UserMonitorView): number | null {
  if (props.window === '7d') {
    return item.availability_7d ?? null
  }
  const detail = props.detailCache[item.id]
  if (!detail) return null
  const primary = detail.models.find(m => m.model === item.primary_model)
  if (!primary) return null
  return props.window === '15d' ? primary.availability_15d ?? null : primary.availability_30d ?? null
}
</script>
