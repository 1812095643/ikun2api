<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import MonitorPager from './MonitorPager.vue'
import type { MonitorIncident, MonitorNames, MonitorRule } from '@/api/admin/monitor'
import { ago, hasTime, incidentTitle, inWindow, isOpen, scopeParts, severityMeta, span, standingProblem, statusMeta, topLevel, windowLabel, type RecentWindow } from './monitorView'
const props = defineProps<{ incidents: MonitorIncident[]; rules: MonitorRule[]; names?: MonitorNames; selected: string | null; now: number; windowMin: RecentWindow }>()
const emit = defineEmits<{ open: [id: string] }>()
const PAGE = 10
const query = ref(''); const showResolved = ref(false)
const pages = reactive<Record<string, number>>({ open: 1, standing: 1, done: 1 })
watch([query, showResolved, () => props.windowMin], () => { pages.open = 1; pages.standing = 1; pages.done = 1 })

// Members of an account/group rollup and digest-only items are reported elsewhere.
const top = computed(() => topLevel(props.incidents))
const recent = (i: MonitorIncident) => inWindow(i, props.now, props.windowMin)
const standing = (i: MonitorIncident) => standingProblem(i, props.rules, props.now, props.windowMin)
const matches = (i: MonitorIncident) => {
  const q = query.value.trim().toLowerCase()
  if (!q) return true
  const text = [incidentTitle(i, props.rules, props.names), i.rule_id, i.scope, ...scopeParts(i, props.names).map(p => p.label), ...(i.explanation?.facts || [])].join(' ').toLowerCase()
  return text.includes(q)
}
const order = (a: MonitorIncident, b: MonitorIncident) => (a.severity === b.severity ? new Date(b.last_seen).getTime() - new Date(a.last_seen).getTime() : a.severity < b.severity ? -1 : 1)
const groups = computed(() => {
  const list = top.value.filter(matches)
  const out = [
    { id: 'open', title: `${windowLabel(props.windowMin)}有报错`, empty: `${windowLabel(props.windowMin)}没有新问题。`, items: list.filter(i => isOpen(i) && recent(i)).sort(order) },
    { id: 'standing', title: '较早开始、尚未确认恢复', empty: '', items: list.filter(standing).sort(order) },
  ]
  if (showResolved.value) out.push({ id: 'done', title: `${windowLabel(props.windowMin)}已恢复`, empty: `${windowLabel(props.windowMin)}没有已恢复的问题。`, items: list.filter(i => i.status === 'resolved' && recent(i)).sort((a, b) => new Date(b.last_seen).getTime() - new Date(a.last_seen).getTime()) })
  return out
})
// Keep the current page valid as items resolve during live refreshes.
watch(groups, gs => { for (const g of gs) pages[g.id] = Math.min(pages[g.id] || 1, Math.max(1, Math.ceil(g.items.length / PAGE))) })
const resolvedCount = computed(() => top.value.filter(i => i.status === 'resolved' && recent(i)).length)
function memberSummary(i: MonitorIncident): string {
  const titles = [...new Set((i.members || []).map(m => props.rules.find(r => r.id === m.rule)?.title || m.rule))]
  const models = [...new Set((i.members || []).map(m => m.model).filter(Boolean))]
  return titles.slice(0, 3).join('、') + (titles.length > 3 ? ` 等 ${titles.length} 类` : '') + (models.length ? `，涉及 ${models.length} 个模型` : '')
}
function lasted(i: MonitorIncident): string {
  const start = new Date(i.first_seen).getTime()
  if (i.status === 'resolved') return `持续了 ${span((hasTime(i.last_fault) ? new Date(i.last_fault!).getTime() : new Date(i.last_seen).getTime()) - start)}`
  return `已持续 ${span(props.now - start)}`
}
function affected(i: MonitorIncident): string {
  const bits: string[] = []
  if (i.user_count) bits.push(`${i.user_count} 位用户`)
  if (i.request_count) bits.push(`${i.request_count} 个请求`)
  return bits.length ? '影响 ' + bits.join('、') : `命中 ${i.count} 次`
}
</script>
<template>
  <section aria-label="需要处理的问题">
    <div class="mb-3 flex flex-wrap items-center gap-3">
      <h2 class="text-base font-semibold text-gray-900 dark:text-white">需要处理</h2>
      <div class="ml-auto flex min-w-0 flex-1 items-center justify-end gap-3 sm:flex-none">
        <input v-model="query" type="search" aria-label="搜索问题" class="input h-9 w-full min-w-0 py-1 text-sm sm:w-64" placeholder="搜索账号、用户、模型或规则">
        <label class="flex shrink-0 items-center gap-1.5 text-sm text-gray-600 dark:text-gray-300"><input v-model="showResolved" type="checkbox" class="rounded">显示已恢复（{{ resolvedCount }}）</label>
      </div>
    </div>
    <div v-for="g in groups" :key="g.id" class="mb-5">
      <h3 v-if="g.items.length || g.empty" class="mb-1.5 text-xs font-medium text-gray-500">{{ g.title }}<span v-if="g.items.length" class="ml-1.5 tabular-nums">{{ g.items.length }}</span></h3>
      <p v-if="!g.items.length && g.empty" class="rounded-xl border border-dashed border-gray-200 px-4 py-5 text-sm text-gray-500 dark:border-dark-700">{{ g.empty }}<template v-if="g.id === 'open'">这不等于所有请求都正常，请同时看上方请求曲线和右侧数据源状态。</template></p>
      <ul class="divide-y divide-gray-100 overflow-hidden rounded-xl border border-gray-200 bg-white dark:divide-dark-800 dark:border-dark-700 dark:bg-dark-900" :class="g.items.length ? '' : 'hidden'">
        <li v-for="i in g.items.slice((pages[g.id] - 1) * PAGE, pages[g.id] * PAGE)" :key="i.id">
          <button type="button" class="group relative flex w-full items-start gap-3 py-3 pl-5 pr-4 text-left transition-colors hover:bg-gray-50 focus-visible:bg-gray-50 focus-visible:outline-none dark:hover:bg-dark-800/70 dark:focus-visible:bg-dark-800" :class="selected === i.id ? 'bg-primary-50/60 dark:bg-primary-950/30' : ''" :aria-label="`查看：${incidentTitle(i, rules, names)}`" @click="emit('open', i.id)">
            <span class="absolute inset-y-0 left-0 w-1" :class="severityMeta[i.severity]?.rule" aria-hidden="true" />
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                <span class="rounded px-1.5 py-0.5 text-[11px] font-medium" :class="severityMeta[i.severity]?.pill">{{ severityMeta[i.severity]?.label || i.severity }}</span>
                <span class="font-medium text-gray-900 dark:text-white">{{ incidentTitle(i, rules, names) }}</span>
              </div>
              <div class="mt-1.5 flex flex-wrap gap-1.5">
                <span v-for="p in scopeParts(i, names)" :key="p.kind + p.label" class="max-w-[16rem] truncate rounded-md bg-gray-100 px-1.5 py-0.5 text-xs text-gray-700 dark:bg-dark-800 dark:text-gray-300" :title="p.title">{{ p.label }}</span>
              </div>
              <p v-if="i.rollup && i.members?.length" class="mt-1.5 truncate text-xs text-gray-600 dark:text-gray-400">包括 {{ memberSummary(i) }}</p>
              <p class="mt-1.5 text-xs text-gray-500">
                <span>{{ affected(i) }}</span><span class="mx-2 text-gray-300 dark:text-dark-600">|</span><span>{{ lasted(i) }}</span>
                <span v-if="i.acknowledged" class="ml-2 rounded bg-gray-100 px-1 text-gray-600 dark:bg-dark-800 dark:text-gray-300">已确认</span>
                <span v-if="hasTime(i.silenced_until) && new Date(i.silenced_until).getTime() > now" class="ml-2 rounded bg-gray-100 px-1 text-gray-600 dark:bg-dark-800 dark:text-gray-300">静默中</span>
              </p>
            </div>
            <div class="shrink-0 text-right text-xs">
              <p :class="statusMeta[i.status]?.cls">{{ statusMeta[i.status]?.label || i.status }}</p>
              <p class="mt-1 tabular-nums text-gray-400">{{ ago(hasTime(i.last_fault) ? i.last_fault : i.last_seen, now) }}</p>
            </div>
          </button>
        </li>
      </ul>
      <MonitorPager v-model="pages[g.id]" :total="g.items.length" :size="PAGE" :label="g.title" />
    </div>
  </section>
</template>
