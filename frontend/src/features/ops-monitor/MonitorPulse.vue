<script setup lang="ts">
import { computed } from 'vue'
import type { MonitorIncident, MonitorLive, MonitorRule } from '@/api/admin/monitor'
import { accountsOf, inWindow, isOpen, recentWindows, seconds, standingProblem, topLevel, windowLabel, type RecentWindow } from './monitorView'
const props = defineProps<{ live: MonitorLive | null; incidents: MonitorIncident[]; rules: MonitorRule[]; stale: string[]; liveError: string; windowMin: RecentWindow; now: number }>()
const emit = defineEmits<{ 'update:windowMin': [value: RecentWindow] }>()

// Only recent faults are actionable: an API problem that is still happening keeps failing.
const top = computed(() => topLevel(props.incidents))
const recent = computed(() => top.value.filter(i => inWindow(i, props.now, props.windowMin)))
const active = computed(() => recent.value.filter(isOpen))
const serious = computed(() => active.value.filter(i => i.severity <= 'P1').length)
const accounts = computed(() => accountsOf(active.value).size)
const recovered = computed(() => recent.value.filter(i => i.status === 'resolved').length)
const standing = computed(() => top.value.filter(i => standingProblem(i, props.rules, props.now, props.windowMin)).length)
// The band's tone is the page's at-a-glance answer to "is anything wrong right now?".
const tone = computed(() => serious.value ? 'fault' : active.value.length ? 'warn' : standing.value ? 'watch' : 'calm')
const label = computed(() => windowLabel(props.windowMin))
const headline = computed(() => {
  if (!active.value.length) return recovered.value ? `${label.value}没有未恢复的问题，${recovered.value} 个已恢复` : `${label.value}没有新问题`
  let text = `${label.value}有 ${active.value.length} 个问题`
  if (serious.value) text += `，其中 ${serious.value} 个严重`
  if (accounts.value) text += `，涉及 ${accounts.value} 个上游账号`
  return text
})
const t = computed(() => props.live?.totals)
const failRate = computed(() => (t.value && t.value.requests ? (t.value.failed / t.value.requests) * 100 : 0))
const summary = computed(() => {
  if (!props.live || !t.value) return props.liveError ? '实时请求数据暂时读不到：' + props.liveError : '正在读取最近一小时的请求…'
  const parts = [`过去 60 分钟完成 ${t.value.requests} 个请求`]
  parts.push(t.value.failed ? `失败 ${t.value.failed} 个（${failRate.value.toFixed(1)}%）` : '没有失败')
  if (t.value.upstream_errors) parts.push(`上游报错 ${t.value.upstream_errors} 次（已重试或换号）`)
  parts.push(`现在有 ${props.live.inflight_count} 个请求在进行`)
  if (props.live.first_output_p50_ms) parts.push(`首个输出中位 ${seconds(props.live.first_output_p50_ms)}`)
  return parts.join('，') + '。'
})
const peak = computed(() => Math.max(4, ...(props.live?.buckets || []).map(b => b.requests)))
const bars = computed(() => (props.live?.buckets || []).map((b, n) => {
  const h = (v: number) => (v / peak.value) * 100
  const at = new Date(b.at)
  const label = at.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
  return { key: b.at, n, ok: h(b.succeeded), cancel: h(b.cancelled), fail: h(b.failed), upstream: b.upstream_errors, empty: !b.requests, tip: `${label}：完成 ${b.requests}，失败 ${b.failed}，取消 ${b.cancelled}，上游报错 ${b.upstream_errors}` }
}))
const ticks = computed(() => {
  const b = props.live?.buckets || []
  return [0, 15, 30, 45].filter(n => b[n]).map(n => ({ n, label: new Date(b[n].at).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false }) }))
})
const toneClass: Record<string, string> = {
  fault: 'border-red-200 bg-gradient-to-b from-red-50 to-white dark:border-red-900/70 dark:from-red-950/50 dark:to-dark-900',
  warn: 'border-amber-200 bg-gradient-to-b from-amber-50 to-white dark:border-amber-900/60 dark:from-amber-950/40 dark:to-dark-900',
  watch: 'border-amber-100 bg-gradient-to-b from-amber-50/60 to-white dark:border-dark-700 dark:from-amber-950/20 dark:to-dark-900',
  calm: 'border-primary-100 bg-gradient-to-b from-primary-50/70 to-white dark:border-dark-700 dark:from-primary-950/40 dark:to-dark-900',
}
const dotClass: Record<string, string> = { fault: 'bg-red-500', warn: 'bg-amber-500', watch: 'bg-amber-400', calm: 'bg-primary-500' }
</script>
<template>
  <section class="rounded-2xl border px-5 pb-4 pt-5 sm:px-7" :class="toneClass[tone]" aria-label="当前状况">
    <div class="flex flex-wrap items-start gap-3">
      <span class="mt-2.5 h-2.5 w-2.5 shrink-0 rounded-full" :class="dotClass[tone]" aria-hidden="true" />
      <div class="min-w-0 flex-1">
        <h2 class="text-xl font-semibold tracking-tight text-gray-900 sm:text-2xl dark:text-white" data-testid="headline">{{ headline }}</h2>
        <p class="mt-1.5 max-w-3xl text-sm leading-6 text-gray-600 dark:text-gray-300">{{ summary }}</p>
        <p v-if="standing" class="mt-1 text-sm text-gray-600 dark:text-gray-300">另有 {{ standing }} 个较早开始的状态问题（如账号隔离、订单未发放）尚未确认恢复，见下方列表。</p>
        <p v-if="stale.length" class="mt-1 text-sm text-amber-700 dark:text-amber-300">{{ stale.join('、') }} 超过 2 分钟没有更新，以上结论可能不完整。</p>
      </div>
      <div class="flex shrink-0 rounded-lg bg-white/70 p-0.5 ring-1 ring-inset ring-gray-200 dark:bg-dark-800/70 dark:ring-dark-700" role="group" aria-label="统计时间范围">
        <button v-for="m in recentWindows" :key="m" type="button" class="rounded-md px-2.5 py-1 text-xs tabular-nums transition-colors" :class="windowMin === m ? 'bg-gray-900 text-white dark:bg-white dark:text-gray-900' : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'" :aria-pressed="windowMin === m" @click="emit('update:windowMin', m)">{{ windowLabel(m).replace('近 ', '') }}</button>
      </div>
    </div>
    <figure v-if="live" class="mt-5">
      <div class="relative flex h-24 items-end gap-[2px]" role="img" :aria-label="`过去 60 分钟每分钟请求量：${summary}`">
        <div v-for="b in bars" :key="b.key" class="group relative flex h-full flex-1 flex-col justify-end" :title="b.tip">
          <span v-if="b.upstream" class="absolute left-1/2 top-0 h-1 w-1 -translate-x-1/2 rounded-full bg-amber-500" />
          <span v-if="b.empty" class="h-px w-full bg-gray-200 dark:bg-dark-700" />
          <span class="w-full rounded-t-[2px] bg-red-500" :style="{ height: b.fail + '%' }" />
          <span class="w-full bg-slate-300 dark:bg-slate-600" :style="{ height: b.cancel + '%' }" />
          <span class="w-full bg-primary-400/80 group-hover:bg-primary-500 dark:bg-primary-600/80" :class="b.fail || b.cancel ? '' : 'rounded-t-[2px]'" :style="{ height: b.ok + '%' }" />
        </div>
      </div>
      <figcaption class="relative mt-1.5 h-4 text-[11px] tabular-nums text-gray-400">
        <span v-for="tk in ticks" :key="tk.n" class="absolute" :style="{ left: (tk.n / 60) * 100 + '%' }">{{ tk.label }}</span>
        <span class="absolute right-0">现在</span>
      </figcaption>
      <div class="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500">
        <span class="inline-flex items-center gap-1.5"><i class="h-2 w-2 rounded-sm bg-primary-400" />完成</span>
        <span class="inline-flex items-center gap-1.5"><i class="h-2 w-2 rounded-sm bg-red-500" />失败</span>
        <span class="inline-flex items-center gap-1.5"><i class="h-2 w-2 rounded-sm bg-slate-300 dark:bg-slate-600" />用户取消</span>
        <span class="inline-flex items-center gap-1.5"><i class="h-1.5 w-1.5 rounded-full bg-amber-500" />这一分钟有上游报错</span>
      </div>
    </figure>
  </section>
</template>
