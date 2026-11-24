<script setup lang="ts">
import { computed } from 'vue'
import type { MonitorSummary } from '@/api/admin/monitor'
import { ago, sourceName } from './monitorView'
const props = defineProps<{ summary: MonitorSummary; stale: string[]; now: number }>()
const quality: Record<string, string> = { fresh: '完整', partial: '部分字段', stale: '过期', unsupported: '不支持', historical: '历史回补' }
const rows = computed(() => [...props.summary.sources].filter(s => s.source_id !== 'notification').sort((a, b) => Number(props.stale.includes(b.source_id)) - Number(props.stale.includes(a.source_id)) || a.source_id.localeCompare(b.source_id)))
</script>
<template>
  <div class="grid gap-8 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
    <section aria-label="数据源">
      <p class="mb-3 text-sm text-gray-600 dark:text-gray-300">监控靠这些数据源判断问题。某个数据源停止更新时，它负责的那类问题可能发现不了。</p>
      <p v-if="!rows.length" class="rounded-xl border border-dashed border-amber-300 px-4 py-6 text-sm text-amber-800 dark:text-amber-300">还没有任何数据源上报，当前无法判断业务是否正常。</p>
      <ul class="divide-y divide-gray-100 rounded-xl border border-gray-200 bg-white dark:divide-dark-800 dark:border-dark-700 dark:bg-dark-900">
        <li v-for="s in rows" :key="s.source_id" class="flex items-center gap-3 px-4 py-2.5 text-sm">
          <span class="h-2 w-2 shrink-0 rounded-full" :class="stale.includes(s.source_id) ? 'bg-amber-500' : 'bg-primary-500'" />
          <span class="min-w-0 flex-1 truncate text-gray-800 dark:text-gray-200" :title="s.source_id">{{ sourceName(s.source_id) }}</span>
          <span class="hidden text-xs text-gray-500 sm:inline">{{ quality[s.quality] || s.quality }}</span>
          <span v-if="s.dropped" class="text-xs text-amber-700 dark:text-amber-300">丢弃 {{ s.dropped }}</span>
          <span class="w-24 shrink-0 text-right text-xs tabular-nums" :class="stale.includes(s.source_id) ? 'text-amber-700 dark:text-amber-300' : 'text-gray-500'">{{ ago(s.last_success, now) }}</span>
        </li>
      </ul>
    </section>
    <section aria-label="已知盲区" class="space-y-3 text-sm">
      <h3 class="font-semibold text-gray-900 dark:text-white">已知盲区</h3>
      <ul class="space-y-2 text-gray-600 dark:text-gray-300"><li v-for="g in summary.coverage_gaps" :key="g" class="flex gap-2"><span class="mt-2 h-1 w-1 shrink-0 rounded-full bg-gray-400" />{{ g }}</li></ul>
      <div v-if="summary.capabilities.length" class="pt-2">
        <h3 class="mb-2 font-semibold text-gray-900 dark:text-white">网关上报能力</h3>
        <p v-for="c in summary.capabilities" :key="c.name" class="text-xs text-gray-500">{{ c.name }}：{{ quality[c.quality] || c.quality }}<template v-if="c.reason">（{{ c.reason }}）</template></p>
      </div>
    </section>
  </div>
</template>
