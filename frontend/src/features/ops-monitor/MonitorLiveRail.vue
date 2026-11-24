<script setup lang="ts">
import { computed } from 'vue'
import type { MonitorLive, MonitorSummary } from '@/api/admin/monitor'
import { accountErrorsLink, nameOf, seconds, sourceName, userOf } from './monitorView'
const props = defineProps<{ live: MonitorLive | null; summary: MonitorSummary; stale: string[] }>()
const emit = defineEmits<{ view: [tab: string] }>()
const names = computed(() => ({ ...(props.summary.names || {}), ...(props.live?.names || {}) }))
const failedDeliveries = computed(() => props.summary.deliveries.filter(d => d.state === 'dead').length)
const pendingDeliveries = computed(() => props.summary.deliveries.filter(d => ['pending', 'leased'].includes(d.state)).length)
const sources = computed(() => props.summary.sources.filter(s => s.source_id !== 'notification'))
const statusText = (code: string, n: number) => `${code === '传输错误' ? '连接失败' : code} ×${n}`
</script>
<template>
  <aside class="space-y-6" aria-label="实时状态">
    <section>
      <div class="mb-2 flex items-baseline justify-between">
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">正在进行的请求</h2>
        <span class="text-xs tabular-nums text-gray-500">{{ live ? `${live.inflight_count} 个` : '—' }}</span>
      </div>
      <p v-if="!live" class="text-sm text-gray-500">正在读取…</p>
      <p v-else-if="!live.inflight.length" class="rounded-lg bg-gray-50 px-3 py-3 text-sm text-gray-500 dark:bg-dark-800">现在没有请求在进行。</p>
      <ul v-else class="space-y-1.5">
        <li v-for="q in live.inflight.slice(0, 8)" :key="q.request" class="rounded-lg bg-gray-50 px-3 py-2 text-sm dark:bg-dark-800">
          <div class="flex items-center justify-between gap-2">
            <span class="truncate font-medium text-gray-800 dark:text-gray-100">{{ userOf(names, q.user_id, q.key_id)?.label || '未知用户' }}</span>
            <span class="shrink-0 tabular-nums" :class="q.elapsed_ms > 60000 ? 'text-amber-700 dark:text-amber-300' : 'text-gray-500'">{{ seconds(q.elapsed_ms) }}</span>
          </div>
          <div class="mt-0.5 flex flex-wrap items-center gap-x-2 text-xs text-gray-500">
            <span class="truncate">{{ q.model || '模型未知' }}</span>
            <span v-if="q.account_id" class="truncate">经 {{ nameOf(names, 'account', q.account_id) }}</span>
            <span v-else class="text-amber-700 dark:text-amber-300">尚未选到账号</span>
            <span v-if="q.waiting_first_output && q.elapsed_ms > 15000" class="text-amber-700 dark:text-amber-300">还没有输出</span>
          </div>
        </li>
      </ul>
    </section>
    <section>
      <h2 class="text-sm font-semibold text-gray-900 dark:text-white">近 60 分钟报错的上游账号</h2>
      <p class="mb-2 mt-0.5 text-xs text-gray-500">点账号或状态码，在新标签页查看完整报错。</p>
      <p v-if="live && !live.problem_accounts.length" class="rounded-lg bg-gray-50 px-3 py-3 text-sm text-gray-500 dark:bg-dark-800">没有账号报错。</p>
      <ul v-else-if="live" class="divide-y divide-gray-100 dark:divide-dark-800">
        <li v-for="a in live.problem_accounts" :key="a.account_id" class="py-2 text-sm">
          <div class="flex items-center justify-between gap-2">
            <a :href="accountErrorsLink(a.account_id)" target="_blank" rel="noopener" class="truncate font-medium text-gray-800 hover:text-primary-700 hover:underline dark:text-gray-100 dark:hover:text-primary-400" :title="`查看上游账号 #${a.account_id} 的报错`">{{ nameOf(names, 'account', a.account_id) }}</a>
            <span class="shrink-0 text-xs tabular-nums" :class="a.failed ? 'text-red-600 dark:text-red-400' : 'text-gray-500'">{{ a.failed ? `失败 ${a.failed}/${a.requests}` : `完成 ${a.requests}` }}</span>
          </div>
          <div class="mt-1 flex flex-wrap gap-1">
            <a v-for="(n, code) in a.statuses" :key="code" :href="accountErrorsLink(a.account_id, String(code))" target="_blank" rel="noopener" class="rounded bg-amber-50 px-1.5 text-xs tabular-nums text-amber-800 hover:bg-amber-100 dark:bg-amber-950/50 dark:text-amber-300 dark:hover:bg-amber-900/60" :title="`查看这些 ${code} 报错`">{{ statusText(String(code), n) }}</a>
          </div>
        </li>
      </ul>
    </section>
    <section class="space-y-2 text-sm">
      <h2 class="text-sm font-semibold text-gray-900 dark:text-white">通知与数据</h2>
      <button type="button" class="flex w-full items-center justify-between rounded-lg bg-gray-50 px-3 py-2 text-left hover:bg-gray-100 dark:bg-dark-800 dark:hover:bg-dark-700" @click="emit('view', 'deliveries')">
        <span>飞书 {{ summary.transports.feishu ? '已接入' : '未接入' }}<span class="mx-1.5 text-gray-300 dark:text-dark-600">|</span>邮箱 {{ summary.transports.email ? '已接入' : '未接入' }}</span>
        <span :class="failedDeliveries ? 'text-red-600 dark:text-red-400' : 'text-gray-500'" class="text-xs">{{ failedDeliveries ? `${failedDeliveries} 条发送失败` : pendingDeliveries ? `${pendingDeliveries} 条待发` : '发送正常' }}</span>
      </button>
      <button type="button" class="flex w-full items-center justify-between rounded-lg bg-gray-50 px-3 py-2 text-left hover:bg-gray-100 dark:bg-dark-800 dark:hover:bg-dark-700" @click="emit('view', 'sources')">
        <span>{{ sources.length }} 个数据源</span>
        <span class="text-xs" :class="stale.length ? 'text-amber-700 dark:text-amber-300' : 'text-gray-500'">{{ stale.length ? `${stale.length} 个未更新` : '都在更新' }}</span>
      </button>
      <p v-if="summary.config.shadow" class="rounded-lg bg-amber-50 px-3 py-2 text-amber-800 dark:bg-amber-950/50 dark:text-amber-300">影子模式：只记录，不发送通知。<button type="button" class="underline" @click="emit('view', 'settings')">去设置</button></p>
      <p v-for="s in stale.slice(0, 3)" :key="s" class="text-xs text-amber-700 dark:text-amber-300">{{ sourceName(s) }} 没有更新</p>
    </section>
  </aside>
</template>
