<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import MonitorPager from './MonitorPager.vue'
import type { MonitorSummary } from '@/api/admin/monitor'
import { clock, errorClassText, incidentTitle } from './monitorView'
const props = defineProps<{ summary: MonitorSummary }>()
const emit = defineEmits<{ open: [id: string] }>()
const filter = ref<'all' | 'problem'>('all')
const byId = computed(() => new Map(props.summary.incidents.map(i => [i.id, i])))
const kindText: Record<string, string> = { firing: '新问题', update: '更新原卡', resolved: '已恢复', escalated: '升级', reopened: '再次发生', reminder: '提醒' }
const stateMeta: Record<string, { label: string; cls: string }> = {
  accepted: { label: '已送达', cls: 'text-primary-700 dark:text-primary-400' }, pending: { label: '等待发送', cls: 'text-amber-700 dark:text-amber-300' }, leased: { label: '发送中', cls: 'text-amber-700 dark:text-amber-300' },
  dead: { label: '发送失败', cls: 'text-red-600 dark:text-red-400' }, shadow: { label: '影子模式未发送', cls: 'text-gray-500' }, superseded: { label: '已被新版本替代', cls: 'text-gray-500' }, suppressed: { label: '静默中未发送', cls: 'text-gray-500' },
}
const PAGE = 20
const page = ref(1)
watch(filter, () => { page.value = 1 })
const rows = computed(() => props.summary.deliveries.filter(d => filter.value === 'all' || ['dead', 'pending', 'leased'].includes(d.state)))
const shown = computed(() => rows.value.slice((page.value - 1) * PAGE, page.value * PAGE))
</script>
<template>
  <section aria-label="通知记录">
    <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
      <p class="text-sm text-gray-600 dark:text-gray-300">每条记录是一次发往飞书或邮箱的动作。「已送达」表示平台已接收，不代表对方已读。</p>
      <div class="flex rounded-lg bg-gray-100 p-0.5 text-sm dark:bg-dark-800">
        <button v-for="f in [{ id: 'all', t: '全部' }, { id: 'problem', t: '只看失败和待发' }]" :key="f.id" type="button" class="rounded-md px-3 py-1" :class="filter === f.id ? 'bg-white shadow-sm dark:bg-dark-700' : 'text-gray-500'" @click="filter = f.id as 'all' | 'problem'">{{ f.t }}</button>
      </div>
    </div>
    <p v-if="!rows.length" class="rounded-xl border border-dashed border-gray-200 px-4 py-8 text-center text-sm text-gray-500 dark:border-dark-700">{{ filter === 'all' ? '还没有发送记录。确认通知设置里已填写收件人，并关闭了影子模式。' : '没有失败或待发的通知。' }}</p>
    <div v-else class="overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-700">
      <table class="w-full min-w-[640px] text-sm">
        <thead class="bg-gray-50 text-left text-xs text-gray-500 dark:bg-dark-800"><tr><th class="px-4 py-2 font-medium">时间</th><th class="px-4 py-2 font-medium">问题</th><th class="px-4 py-2 font-medium">动作</th><th class="px-4 py-2 font-medium">发往</th><th class="px-4 py-2 font-medium">结果</th></tr></thead>
        <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-900">
          <tr v-for="d in shown" :key="d.id">
            <td class="whitespace-nowrap px-4 py-2 tabular-nums text-gray-500">{{ clock(d.state === 'accepted' ? d.accepted_at : d.next_attempt) }}</td>
            <td class="max-w-[18rem] px-4 py-2"><button type="button" class="truncate text-left text-gray-800 hover:text-primary-700 dark:text-gray-200" @click="emit('open', d.incident_id)">{{ byId.get(d.incident_id) ? incidentTitle(byId.get(d.incident_id)!, summary.rules, summary.names) : '已清理的旧问题' }}</button></td>
            <td class="whitespace-nowrap px-4 py-2 text-gray-600 dark:text-gray-300">{{ kindText[d.kind] || d.kind }}</td>
            <td class="whitespace-nowrap px-4 py-2 text-gray-600 dark:text-gray-300"><span class="mr-1.5 rounded bg-gray-100 px-1 text-xs dark:bg-dark-800">{{ d.channel === 'feishu' ? '飞书' : '邮件' }}</span><span class="text-xs">{{ d.target.length > 18 ? d.target.slice(0, 18) + '…' : d.target }}</span></td>
            <td class="px-4 py-2"><span :class="stateMeta[d.state]?.cls">{{ stateMeta[d.state]?.label || d.state }}</span><span v-if="d.error_class && d.state !== 'accepted'" class="block text-xs text-gray-500">{{ errorClassText[d.error_class] || d.error_class }}<template v-if="d.attempts > 1">，已尝试 {{ d.attempts }} 次</template></span></td>
          </tr>
        </tbody>
      </table>
    </div>
    <MonitorPager v-model="page" :total="rows.length" :size="PAGE" label="通知记录" />
  </section>
</template>
