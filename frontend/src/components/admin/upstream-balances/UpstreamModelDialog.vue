<script setup lang="ts">
import { computed, shallowRef, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import UpstreamModelSettings from './UpstreamModelSettings.vue'
import { useUpstreamModels } from '@/composables/useUpstreamModels'
import { pendingModelChanges } from '@/api/upstreamModels'
import { formatBalanceTime, type UpstreamBalance } from '@/api/upstreamBalances'

const props = defineProps<{ item: UpstreamBalance | null }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const id = computed(() => props.item?.id || '')
const { data, loading, busy, error, load, refresh, save, act } = useUpstreamModels(id, () => emit('changed'))
const tab = shallowRef('changes')
const accountID = shallowRef(0)
const selected = shallowRef<string[]>([])
const search = shallowRef('')
const initialize = shallowRef(false)
const confirming = shallowRef(false)
const account = computed(() => data.value?.accounts.find(a => a.account_id === accountID.value))
const changes = computed(() => pendingModelChanges(account.value, tab.value === 'ignored'))
const rows = computed(() => [
  ...changes.value.added.map(model => ({ model, kind: 'added', label: '新增' })),
  ...changes.value.removed.map(model => ({ model, kind: 'removed', label: '下架' }))
])
const filtered = computed(() => rows.value.filter(r => r.model.toLowerCase().includes(search.value.trim().toLowerCase())))
const options = computed(() => props.item?.accounts.map(a => ({ value: a.id, label: `#${a.id} ${a.name}` })) || [])
const tabs = [{ id: 'changes', label: '待处理' }, { id: 'ignored', label: '已忽略' }, { id: 'history', label: '记录' }, { id: 'settings', label: '监控设置' }]
const labels: Record<string, string> = { detected: '检测到变更', apply: '已应用', ignore: '已忽略', restore: '恢复提醒' }
watch(id, () => { tab.value = 'changes'; accountID.value = props.item?.accounts[0]?.id || 0; search.value = '' }, { immediate: true })
watch([tab, accountID, () => data.value?.revision], () => { selected.value = []; confirming.value = false; initialize.value = false })
function toggle(key: string, checked: boolean) { selected.value = checked ? [...selected.value, key] : selected.value.filter(v => v !== key) }
async function handle(action: 'apply' | 'ignore' | 'restore') {
  const change = { account_id: accountID.value, added: changes.value.added.filter(v => selected.value.includes('added:' + v)), removed: changes.value.removed.filter(v => selected.value.includes('removed:' + v)) }
  if (await act(action, change, initialize.value)) { selected.value = []; confirming.value = false }
}
</script>
<template>
  <BaseDialog :show="!!item" :title="item ? item.name + ' · 模型目录' : '模型目录'" width="wide" :close-on-escape="!busy" :show-close-button="!busy" @close="emit('close')">
    <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 pb-3 dark:border-dark-700">
      <nav aria-label="模型目录标签" class="flex flex-wrap gap-1">
        <button v-for="entry in tabs" :key="entry.id" type="button" class="rounded-lg px-3 py-2 text-sm transition-colors" :aria-current="tab === entry.id ? 'page' : undefined" :class="tab === entry.id ? 'bg-primary-50 font-medium text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'text-gray-500 hover:bg-gray-100 dark:text-dark-400 dark:hover:bg-dark-700'" @click="tab = entry.id">{{ entry.label }}<span v-if="entry.id === 'changes' && data?.summary.pending" class="ml-1.5 tabular-nums">{{ data.summary.pending }}</span></button>
      </nav>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="busy || loading || data?.summary.checking || data?.summary.queued" @click="refresh()"><Icon name="refresh" size="sm" :class="{ 'animate-spin': data?.summary.checking }" />{{ data?.summary.queued ? '已排队' : data?.summary.checking ? '检测中' : '立即检测' }}</button>
    </div>
    <div v-if="error" role="alert" class="mt-4 rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950/20 dark:text-red-300">{{ error }}<button class="ml-2 underline" @click="load()">刷新</button></div>
    <p v-if="loading" role="status" class="py-12 text-center text-sm text-gray-500">加载模型目录…</p>
    <template v-else-if="data">
      <UpstreamModelSettings v-if="tab === 'settings'" class="mx-auto mt-5 max-w-lg" :config="data.config" :busy="busy" @save="save" />
      <ol v-else-if="tab === 'history'" class="mt-5 space-y-4">
        <li v-for="(event, index) in data.history" :key="event.at + index" class="border-b border-gray-100 pb-4 text-sm dark:border-dark-700">
          <div class="flex flex-wrap items-center justify-between gap-2"><span class="font-medium text-gray-900 dark:text-gray-100">{{ labels[event.action] || event.action }}</span><time class="text-xs text-gray-500">{{ formatBalanceTime(event.at) }}</time></div>
          <p v-for="change in event.changes" :key="change.account_id" class="mt-2 text-gray-500 dark:text-dark-400">#{{ change.account_id }} · 新增 {{ change.added.length }} / 下架 {{ change.removed.length }}<span v-if="event.actor_id"> · 管理员 #{{ event.actor_id }}</span></p>
          <details v-if="event.changes.some(c => c.added.length || c.removed.length)" class="mt-2"><summary class="cursor-pointer text-xs text-gray-500">模型明细</summary><p v-for="change in event.changes" :key="change.account_id" class="mt-2 break-all text-xs leading-5">{{ [...change.added.map(m => '+ ' + m), ...change.removed.map(m => '− ' + m)].join('、') }}</p></details>
        </li>
        <li v-if="!data.history.length" class="py-10 text-center text-sm text-gray-500">暂无变更记录</li>
      </ol>
      <div v-else class="mt-4 space-y-4">
        <div class="flex flex-wrap items-center gap-3"><Select v-model="accountID" :options="options" class="min-w-0 flex-1" aria-label="选择模型来源账号" /><input v-model="search" class="input w-full sm:w-56" aria-label="筛选模型" placeholder="筛选模型" /></div>
        <div class="flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-dark-400"><span>{{ account?.current?.length || 0 }} 个模型</span><span>最近成功检测：{{ formatBalanceTime(account?.checked_at || null) }}</span></div>
        <p v-if="account?.error" role="alert" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-700 dark:bg-amber-900/15 dark:text-amber-300">{{ account.error }}</p>
        <p v-if="data.notification_error" class="text-xs text-amber-600">{{ data.notification_error }}</p>
        <div v-if="rows.length" class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-700">
          <div class="flex items-center justify-between border-b border-gray-200 bg-gray-50 px-4 py-2 dark:border-dark-700 dark:bg-dark-900/40"><label class="flex items-center gap-2 text-sm"><input type="checkbox" :checked="!!filtered.length && filtered.every(r => selected.includes(r.kind + ':' + r.model))" @change="selected = ($event.target as HTMLInputElement).checked ? filtered.map(r => r.kind + ':' + r.model) : []" />选择当前结果</label><span class="text-xs text-gray-500">已选 {{ selected.length }}</span></div>
          <div class="max-h-72 overflow-y-auto"><label v-for="row in filtered" :key="row.kind + row.model" class="flex items-center gap-3 border-b border-gray-100 px-4 py-3 last:border-0 dark:border-dark-700"><input type="checkbox" :checked="selected.includes(row.kind + ':' + row.model)" @change="toggle(row.kind + ':' + row.model, ($event.target as HTMLInputElement).checked)" /><span class="shrink-0 rounded px-1.5 py-0.5 text-xs" :class="row.kind === 'added' ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300'">{{ row.label }}</span><span class="min-w-0 break-all text-sm text-gray-800 dark:text-dark-100">{{ row.model }}</span></label></div>
        </div>
        <div v-else class="py-10 text-center"><Icon name="server" size="xl" class="mx-auto mb-3 text-gray-400" /><p class="text-sm text-gray-600 dark:text-dark-300">{{ !account?.checked_at ? '点击立即检测，建立模型基线' : tab === 'ignored' ? '没有已忽略的变更' : '暂无待处理的模型变更' }}</p></div>
        <div v-if="confirming" class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
          <p class="text-sm text-gray-700 dark:text-dark-200">将更新账号 #{{ accountID }} 的模型映射。自定义别名和通配符冲突会被拦截，分组与渠道价格需单独配置。</p>
          <label class="mt-3 flex items-start gap-2 text-xs leading-5 text-gray-500 dark:text-dark-400"><input v-model="initialize" type="checkbox" class="mt-1" />若账号尚无模型名单，允许按本次检测基线建立名单（会从允许所有模型切换为名单模式）</label>
          <div class="mt-4 flex justify-end gap-2"><button class="btn btn-secondary btn-sm" :disabled="busy" @click="confirming = false">取消</button><button class="btn btn-primary btn-sm" :disabled="busy" @click="handle('apply')">确认应用 {{ selected.length }} 项</button></div>
        </div>
        <div v-else class="flex flex-wrap justify-end gap-2"><template v-if="tab === 'changes'"><button class="btn btn-secondary" :disabled="!selected.length || busy" @click="handle('ignore')">忽略所选</button><button class="btn btn-primary" :disabled="!selected.length || busy || !!account?.error" @click="confirming = true">应用所选</button></template><button v-else class="btn btn-secondary" :disabled="!selected.length || busy" @click="handle('restore')">恢复提醒</button></div>
      </div>
    </template>
  </BaseDialog>
</template>
