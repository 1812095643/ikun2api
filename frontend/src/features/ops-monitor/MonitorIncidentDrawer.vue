<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { monitorAPI, monitorError, type MonitorDetail, type MonitorNames, type MonitorRule } from '@/api/admin/monitor'
import { ago, clock, hasTime, incidentTitle, nameOf, scopeParts, severityMeta, span, statusMeta, userOf } from './monitorView'
const props = defineProps<{ id: string | null; rules: MonitorRule[]; names?: MonitorNames; tick: number }>()
const emit = defineEmits<{ close: []; changed: []; open: [id: string] }>()
const detail = ref<MonitorDetail | null>(null); const error = ref(''); const busy = ref(false)
const pending = ref<'' | 'silence' | 'close'>(''); const reason = ref(''); const panel = ref<HTMLElement | null>(null)
const names = computed(() => ({ ...(props.names || {}), ...(detail.value?.names || {}) }))
const inc = computed(() => detail.value?.incident)

async function load(id: string, quiet = false) {
  if (!quiet) { busy.value = true; error.value = '' }
  try { const d = await monitorAPI.detail(id); if (props.id === id) detail.value = d } catch (e) { if (!quiet) error.value = monitorError(e) } finally { if (!quiet) busy.value = false }
}
watch(() => props.id, async id => {
  detail.value = null; pending.value = ''; reason.value = ''; error.value = ''
  if (!id) return
  await load(id); await nextTick(); panel.value?.focus()
}, { immediate: true })
// Live: refresh with the page, unless the admin is in the middle of an action.
watch(() => props.tick, () => { if (props.id && !busy.value && !pending.value) load(props.id, true) })
function onKey(e: KeyboardEvent) { if (e.key === 'Escape' && props.id) emit('close') }
window.addEventListener('keydown', onKey)
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))

async function act(action: string) {
  if (!detail.value) return
  if ((action === 'silence' || action === 'close') && !reason.value.trim()) { error.value = action === 'silence' ? '请写一句静默原因，方便其他管理员了解' : '请写一句关闭原因'; return }
  busy.value = true; error.value = ''
  try {
    detail.value = { ...detail.value, ...(await monitorAPI.control(detail.value.incident, action, reason.value.trim(), action === 'silence' ? new Date(Date.now() + 3600000).toISOString() : undefined)) }
    pending.value = ''; reason.value = ''; emit('changed')
  } catch (e) { error.value = monitorError(e) } finally { busy.value = false }
}

const silenced = computed(() => inc.value && hasTime(inc.value.silenced_until) && new Date(inc.value.silenced_until).getTime() > Date.now())
const users = computed(() => (detail.value?.impact?.users || []).map(id => userOf(names.value, id)).filter(Boolean) as { label: string; email?: string }[])
const accounts = computed(() => (detail.value?.impact?.accounts || []).map(id => ({ id, label: nameOf(names.value, 'account', id) })))
// 24h of evidence in 30-minute buckets: fault hits above the axis, recovery samples as teal ticks.
const hours = computed(() => {
  const end = Date.now(); const start = end - 24 * 3600000; const size = 30 * 60000
  const b = Array.from({ length: 48 }, (_, n) => ({ n, at: start + n * size, fault: 0, ok: 0 }))
  for (const h of detail.value?.hits || []) {
    const n = Math.floor((new Date(h.at).getTime() - start) / size)
    if (n >= 0 && n < 48) h.healthy ? b[n].ok++ : b[n].fault++
  }
  const peak = Math.max(1, ...b.map(x => x.fault))
  return { bars: b.map(x => ({ ...x, h: (x.fault / peak) * 100, tip: `${new Date(x.at).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })} 起 30 分钟：异常 ${x.fault} 次，正常 ${x.ok} 次` })), total: b.reduce((s, x) => s + x.fault, 0), peak }
})
const history = computed(() => {
  const out: { at: string; status: string; severity: string }[] = []
  for (const h of [...(detail.value?.history || [])].reverse()) {
    const last = out[out.length - 1]
    if (!last || last.status !== h.status || last.severity !== h.severity) out.push({ at: h.last_seen, status: h.status, severity: h.severity })
  }
  return out.reverse().slice(0, 30)
})
// Rollup facts repeat the member list in a terse form; show members readably and keep the latest evidence.
const facts = computed(() => {
  const i = inc.value
  if (!i) return []
  if (!i.rollup) return i.explanation.facts
  const members = (i.members || []).map(m => `${props.rules.find(r => r.id === m.rule)?.title || m.rule}${m.model ? `（${m.model}）` : ''}：本轮 ${m.count} 次`)
  const latest = i.explanation.facts.filter(f => f.startsWith('最近一条')).map(f => f.replace(/^最近一条（[A-Z]\d+）：/, '最近一次：'))
  return [...members, ...latest]
})
const confidence: Record<string, string> = { high: '高', medium: '中', low: '低' }
</script>
<template>
  <Teleport to="body">
    <Transition enter-from-class="opacity-0" leave-to-class="opacity-0" enter-active-class="transition-opacity duration-150" leave-active-class="transition-opacity duration-150">
      <div v-if="id" class="fixed inset-0 z-40 bg-slate-900/30 dark:bg-black/50" aria-hidden="true" @click="emit('close')" />
    </Transition>
    <Transition enter-from-class="translate-x-full" leave-to-class="translate-x-full" enter-active-class="transition-transform duration-200 ease-out motion-reduce:transition-none" leave-active-class="transition-transform duration-150 ease-in motion-reduce:transition-none">
      <div v-if="id" ref="panel" tabindex="-1" role="dialog" aria-modal="true" aria-label="问题详情" class="fixed inset-y-0 right-0 z-50 flex w-full max-w-2xl flex-col bg-white shadow-2xl outline-none dark:bg-dark-900">
        <header class="border-b border-gray-200 px-5 pb-4 pt-4 sm:px-6 dark:border-dark-700">
          <div class="flex items-start gap-3">
            <div class="min-w-0 flex-1">
              <div v-if="inc" class="flex flex-wrap items-center gap-2 text-xs">
                <span class="rounded px-1.5 py-0.5 font-medium" :class="severityMeta[inc.severity]?.pill">{{ severityMeta[inc.severity]?.label || inc.severity }}</span>
                <span class="font-medium" :class="statusMeta[inc.status]?.cls">{{ inc.status === 'resolved' && inc.closed_by === 'quiet' ? '2 小时无新报错，已自动关闭' : statusMeta[inc.status]?.label || inc.status }}</span>
                <span v-if="inc.acknowledged" class="rounded bg-gray-100 px-1.5 py-0.5 text-gray-600 dark:bg-dark-800 dark:text-gray-300">已确认</span>
                <span v-if="silenced" class="rounded bg-gray-100 px-1.5 py-0.5 text-gray-600 dark:bg-dark-800 dark:text-gray-300">静默到 {{ clock(inc.silenced_until) }}</span>
              </div>
              <h2 class="mt-2 text-lg font-semibold leading-snug text-gray-900 dark:text-white">{{ inc ? incidentTitle(inc, rules, names) : '问题详情' }}</h2>
              <div v-if="inc" class="mt-2 flex flex-wrap gap-1.5">
                <span v-for="p in scopeParts(inc, names)" :key="p.kind + p.label" class="rounded-md bg-gray-100 px-1.5 py-0.5 text-xs text-gray-700 dark:bg-dark-800 dark:text-gray-300" :title="p.title">{{ p.label }}</span>
              </div>
            </div>
            <button type="button" class="rounded-lg p-2 text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-800" aria-label="关闭详情" @click="emit('close')"><svg class="h-5 w-5" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path d="M6.28 5.22a.75.75 0 0 0-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 1 0 1.06 1.06L10 11.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L11.06 10l3.72-3.72a.75.75 0 0 0-1.06-1.06L10 8.94 6.28 5.22Z" /></svg></button>
          </div>
          <dl v-if="inc" class="mt-3 grid grid-cols-3 gap-3 text-xs">
            <div><dt class="text-gray-500">开始</dt><dd class="mt-0.5 tabular-nums text-gray-800 dark:text-gray-200">{{ clock(inc.first_seen) }}</dd></div>
            <div><dt class="text-gray-500">最近一次异常</dt><dd class="mt-0.5 tabular-nums text-gray-800 dark:text-gray-200">{{ ago(hasTime(inc.last_fault) ? inc.last_fault : inc.last_seen) }}</dd></div>
            <div><dt class="text-gray-500">{{ inc.status === 'resolved' ? '持续了' : '已持续' }}</dt><dd class="mt-0.5 tabular-nums text-gray-800 dark:text-gray-200">{{ span((inc.status === 'resolved' ? new Date(hasTime(inc.last_fault) ? inc.last_fault! : inc.last_seen).getTime() : Date.now()) - new Date(inc.first_seen).getTime()) }}</dd></div>
          </dl>
          <div v-if="inc" class="mt-4 flex flex-wrap items-center gap-2">
            <button type="button" class="btn btn-secondary h-9 py-0" :disabled="busy" @click="act(inc.acknowledged ? 'unack' : 'ack')">{{ inc.acknowledged ? '取消确认' : '我已看到' }}</button>
            <button v-if="!silenced" type="button" class="btn btn-secondary h-9 py-0" :class="pending === 'silence' ? 'ring-2 ring-primary-300' : ''" :disabled="busy" @click="pending = pending === 'silence' ? '' : 'silence'">静默 1 小时</button>
            <button v-else type="button" class="btn btn-secondary h-9 py-0" :disabled="busy" @click="act('unsilence')">取消静默</button>
            <button v-if="inc.status !== 'resolved'" type="button" class="btn btn-secondary h-9 py-0" :class="pending === 'close' ? 'ring-2 ring-primary-300' : ''" :disabled="busy" @click="pending = pending === 'close' ? '' : 'close'">手动关闭</button>
          </div>
          <form v-if="pending" class="mt-3 flex gap-2" @submit.prevent="act(pending)">
            <input v-model="reason" maxlength="500" class="input h-9 min-w-0 flex-1 py-1 text-sm" :placeholder="pending === 'silence' ? '静默原因，例如：已联系供应商，等待恢复' : '关闭原因，例如：账号已下线'" autofocus>
            <button type="submit" class="btn btn-primary h-9 py-0" :disabled="busy">{{ pending === 'silence' ? '确认静默' : '确认关闭' }}</button>
          </form>
          <p v-if="error" role="alert" class="mt-3 text-sm text-red-600">{{ error }}</p>
        </header>

        <div class="flex-1 space-y-7 overflow-y-auto px-5 py-5 text-sm sm:px-6">
          <p v-if="busy && !detail" class="text-gray-500">正在读取…</p>
          <template v-if="inc && detail">
            <section>
              <h3 class="mb-2 font-semibold text-gray-900 dark:text-white">影响范围</h3>
              <p class="text-gray-600 dark:text-gray-300"><span class="tabular-nums">{{ inc.request_count }}</span> 个请求，<span class="tabular-nums">{{ inc.user_count }}</span> 位用户，<span class="tabular-nums">{{ inc.account_count }}</span> 个上游账号</p>
              <div v-if="users.length" class="mt-2.5 flex flex-wrap gap-1.5">
                <span v-for="u in users.slice(0, 16)" :key="u.label" class="inline-flex items-baseline gap-1 rounded-full bg-gray-100 px-2.5 py-1 text-xs text-gray-800 dark:bg-dark-800 dark:text-gray-200">{{ u.label }}<span v-if="u.email" class="text-gray-500">{{ u.email }}</span></span>
                <span v-if="users.length > 16" class="px-1 py-1 text-xs text-gray-500">另有 {{ users.length - 16 }} 位</span>
              </div>
              <div v-if="accounts.length" class="mt-2 flex flex-wrap gap-1.5">
                <span v-for="a in accounts.slice(0, 12)" :key="a.id" class="rounded-full px-2.5 py-1 text-xs text-gray-700 ring-1 ring-inset ring-gray-200 dark:text-gray-300 dark:ring-dark-700" :title="`上游账号 #${a.id}`">{{ a.label }}</span>
              </div>
            </section>

            <section>
              <div class="mb-2 flex items-baseline justify-between"><h3 class="font-semibold text-gray-900 dark:text-white">近 24 小时</h3><span class="text-xs text-gray-500">异常 {{ hours.total }} 次</span></div>
              <div class="flex h-16 items-end gap-[2px]" role="img" :aria-label="`近 24 小时异常 ${hours.total} 次`">
                <div v-for="b in hours.bars" :key="b.n" class="flex h-full flex-1 flex-col justify-end" :title="b.tip">
                  <span class="w-full rounded-t-sm" :class="b.fault ? (inc.severity <= 'P1' ? 'bg-red-500' : 'bg-amber-500') : 'bg-gray-100 dark:bg-dark-800'" :style="{ height: b.fault ? Math.max(b.h, 6) + '%' : '2px' }" />
                </div>
              </div>
              <div class="mt-1 flex gap-[2px]" aria-hidden="true"><span v-for="b in hours.bars" :key="b.n" class="h-1 flex-1 rounded-full" :class="b.ok ? 'bg-primary-400' : 'bg-transparent'" /></div>
              <div class="mt-1 flex justify-between text-[11px] text-gray-400"><span>24 小时前</span><span>12 小时前</span><span>现在</span></div>
              <p class="mt-1 text-xs text-gray-500">柱子是异常次数，下方青色短线表示这段时间出现过正常请求。</p>
            </section>

            <div class="grid gap-6 sm:grid-cols-2">
              <section>
                <h3 class="mb-2 font-semibold text-gray-900 dark:text-white">发生了什么</h3>
                <ul class="space-y-1.5 text-gray-700 dark:text-gray-300"><li v-for="f in facts" :key="f" class="flex gap-2"><span class="mt-2 h-1 w-1 shrink-0 rounded-full bg-gray-400" />{{ f }}</li></ul>
              </section>
              <section>
                <h3 class="mb-2 font-semibold text-gray-900 dark:text-white">建议怎么处理</h3>
                <ul class="space-y-1.5 text-gray-700 dark:text-gray-300"><li v-for="a in inc.explanation.advice" :key="a" class="flex gap-2"><span class="mt-2 h-1 w-1 shrink-0 rounded-full bg-primary-500" />{{ a }}</li></ul>
              </section>
            </div>

            <section v-if="detail.children?.length">
              <h3 class="mb-2 font-semibold text-gray-900 dark:text-white">合并在这里的问题<span class="ml-1.5 text-xs font-normal text-gray-500">{{ detail.children.length }}</span></h3>
              <ul class="divide-y divide-gray-100 rounded-xl border border-gray-200 dark:divide-dark-800 dark:border-dark-700">
                <li v-for="c in detail.children.slice(0, 30)" :key="c.id">
                  <button type="button" class="flex w-full items-center gap-3 px-3 py-2 text-left hover:bg-gray-50 dark:hover:bg-dark-800" @click="emit('open', c.id)">
                    <span class="h-6 w-0.5 rounded-full" :class="severityMeta[c.severity]?.rule" />
                    <span class="min-w-0 flex-1"><span class="block truncate text-gray-800 dark:text-gray-200">{{ rules.find(r => r.id === c.rule_id)?.title || c.rule_id }}</span><span class="block truncate text-xs text-gray-500">{{ scopeParts(c, names).map(p => p.label).join('，') }}</span></span>
                    <span class="shrink-0 text-xs tabular-nums text-gray-500">{{ c.count }} 次</span>
                    <span class="w-16 shrink-0 text-right text-xs" :class="statusMeta[c.status]?.cls">{{ statusMeta[c.status]?.label }}</span>
                  </button>
                </li>
              </ul>
            </section>

            <section class="rounded-xl bg-gray-50 px-4 py-3 text-xs leading-5 text-gray-600 dark:bg-dark-800 dark:text-gray-400">
              <p><span class="font-medium text-gray-700 dark:text-gray-300">判断把握：</span>{{ confidence[inc.explanation.confidence] || '未知' }}</p>
              <p v-for="m in inc.explanation.missing_evidence" :key="m">{{ m }}</p>
              <p v-for="x in inc.explanation.inferences" :key="x">{{ x }}</p>
            </section>

            <details class="group">
              <summary class="cursor-pointer select-none font-semibold text-gray-900 dark:text-white">状态变化记录<span class="ml-1.5 text-xs font-normal text-gray-500">{{ history.length }}</span></summary>
              <ol class="mt-3 space-y-2 border-l border-gray-200 pl-4 dark:border-dark-700">
                <li v-for="h in history" :key="h.at + h.status" class="relative text-xs">
                  <span class="absolute -left-[21px] top-1 h-2 w-2 rounded-full" :class="severityMeta[h.severity]?.rule" />
                  <span class="tabular-nums text-gray-500">{{ clock(h.at) }}</span><span class="ml-2" :class="statusMeta[h.status]?.cls">{{ statusMeta[h.status]?.label || h.status }}</span><span class="ml-2 text-gray-500">{{ severityMeta[h.severity]?.label }}</span>
                </li>
              </ol>
            </details>
            <p class="text-[11px] text-gray-400">规则 {{ inc.rule_id }} v{{ inc.rule_version }}，事件版本 {{ inc.revision }}，编号 {{ inc.id.slice(0, 16) }}</p>
          </template>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
