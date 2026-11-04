<script setup lang="ts">
import { computed, ref, shallowRef } from 'vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Toggle from '@/components/common/Toggle.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Pagination from '@/components/common/Pagination.vue'
import WarmupRuleDialog from './WarmupRuleDialog.vue'
import WarmupRunTable from './WarmupRunTable.vue'
import { useAccountWarmup } from '@/composables/useAccountWarmup'
import { accountWarmupAPI, type WarmupPreview, type WarmupRule } from '@/api/accountWarmup'
const { state, runs, selected, page, pageSize, total, loading, busy, error, load, action, save } = useAccountWarmup()
const tab = shallowRef('rules'), editing = shallowRef(false), draft = ref<WarmupRule | null>(null)
const search = shallowRef(''), mode = shallowRef('inherit'), overrideRule = shallowRef<number | null>(null)
const preview = ref<WarmupPreview | null>(null), previewRule = shallowRef<WarmupRule | null>(null), deleteRule = shallowRef<WarmupRule | null>(null)
let requestID = ''
const filteredAccounts = computed(() => state.value.accounts.filter(a => (a.name + ' ' + a.id).toLowerCase().includes(search.value.toLowerCase())))
const overrideOptions = computed(() => state.value.rules.map(r => ({ value: r.id, label: r.name })))
const accountColumns = [{ key: 'name', label: '账号' }, { key: 'status', label: '当前状态' }, { key: 'rule_name', label: '生效规则' }, { key: 'override', label: '账号策略' }, { key: 'window_end', label: '已记录窗口' }]
const ruleColumns = [{ key: 'name', label: '规则' }, { key: 'schedule', label: '执行时间' }, { key: 'requests', label: '请求设置' }, { key: 'enabled', label: '启用' }, { key: 'actions', label: '操作' }]
function edit(rule?: WarmupRule) { draft.value = rule ?? null; editing.value = true }
async function onSave(r: WarmupRule) { if (await save(r)) { editing.value = false; draft.value = null } }
async function showPreview(r: WarmupRule) { await action(async () => { preview.value = await accountWarmupAPI.preview(r.id); previewRule.value = r; requestID = crypto.randomUUID() }, '预览已生成') }
async function run() { if (previewRule.value && await action(() => accountWarmupAPI.run(previewRule.value!.id, requestID), '已加入串行热身队列')) { preview.value = null; tab.value = 'runs' } }
async function override() { if (await action(() => accountWarmupAPI.overrides(selected.value.map(Number), mode.value, overrideRule.value))) selected.value = [] }
async function remove() { if (deleteRule.value && await action(() => accountWarmupAPI.delete(deleteRule.value!.id), '规则已删除')) deleteRule.value = null }
const time = (s: string | null) => s ? new Date(s).toLocaleString() : '—'
</script>
<template>
 <TablePageLayout>
  <template #filters>
   <div class="flex flex-wrap items-center justify-between gap-3"><div class="flex gap-1 rounded-lg bg-gray-100 p-1 dark:bg-dark-800"><button v-for="item in [{ key: 'rules', label: '热身规则' }, { key: 'accounts', label: '账号策略' }, { key: 'runs', label: '执行记录' }]" :key="item.key" class="rounded-md px-4 py-2 text-sm" :class="tab === item.key ? 'bg-white text-primary-600 shadow-sm dark:bg-dark-700 dark:text-primary-400' : 'text-gray-500'" @click="tab = item.key">{{ item.label }}</button></div><div class="flex items-center gap-3"><label class="flex items-center gap-2 text-sm"><span>自动热身</span><Toggle :model-value="state.settings.enabled" :disabled="busy" @update:model-value="action(() => accountWarmupAPI.settings($event))" /></label><button class="btn btn-secondary" :disabled="loading" @click="load">刷新</button><button class="btn btn-secondary" :disabled="busy" @click="action(accountWarmupAPI.cancel, '已取消热身队列')">停止本轮</button><button v-if="tab === 'rules'" class="btn btn-primary" @click="edit()">新建规则</button></div></div>
   <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">执行前串行主动刷新额度；429、五小时或周用量 &gt;99% 的账号跳过。热身并发 1，用户业务优先。</p>
   <p v-if="error" role="alert" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
   <div v-if="tab === 'accounts'" class="mt-4 flex flex-wrap items-center gap-2"><input v-model="search" class="input w-60" placeholder="搜索账号或 ID" aria-label="搜索账号" /><span class="text-xs text-gray-500">{{ filteredAccounts.length }} 个账号</span><template v-if="selected.length"><Select v-model="mode" class="w-36" :options="[{ value: 'inherit', label: '继承规则' }, { value: 'disabled', label: '排除热身' }, { value: 'rule', label: '指定规则' }]" /><Select v-if="mode === 'rule'" v-model="overrideRule" class="w-44" :options="overrideOptions" placeholder="选择规则" /><button class="btn btn-secondary" :disabled="busy || (mode === 'rule' && !overrideRule)" @click="override">应用到 {{ selected.length }} 个账号</button></template></div>
  </template>
  <template #table>
   <DataTable v-if="tab === 'rules'" :columns="ruleColumns" :data="state.rules" :loading="loading" row-key="id" :expandable-actions="false">
    <template #cell-name="{ row }"><span class="font-medium">{{ row.name }}</span><p class="mt-1 text-xs text-gray-500">优先级 {{ row.priority }} · {{ row.config.account_ids?.length ? '指定账号' : row.config.group_ids?.length ? '指定分组' : '全局筛选' }}</p></template>
    <template #cell-schedule="{ row }"><div class="text-xs"><p v-for="(window, index) in row.config.windows" :key="index">{{ window.start }}–{{ window.end }} · {{ window.rounds }} 轮</p><p class="mt-1 text-gray-500">{{ row.config.timezone }}{{ row.config.date ? ' · ' + row.config.date : '' }}</p></div></template>
    <template #cell-requests="{ row }"><p class="max-w-52 truncate text-xs">{{ row.config.model }}</p><p class="mt-1 text-xs text-gray-500">每轮 {{ row.config.count }} 条 · 间隔 {{ row.config.message_gap }} 秒</p></template>
    <template #cell-enabled="{ row }"><Toggle :model-value="row.enabled" :disabled="busy" @update:model-value="action(() => accountWarmupAPI.save({ ...row, enabled: $event }))" /></template>
    <template #cell-actions="{ row }"><div class="flex gap-2"><button class="btn btn-secondary btn-sm" @click="edit(row)">设置</button><button class="btn btn-secondary btn-sm" :disabled="busy" @click="showPreview(row)">预览</button><button class="btn btn-ghost btn-sm text-red-600" @click="deleteRule = row">删除</button></div></template>
   </DataTable>
   <DataTable v-else-if="tab === 'accounts'" :columns="accountColumns" :data="filteredAccounts" :loading="loading" row-key="id" selectable v-model:selected-keys="selected">
    <template #cell-name="{ row }"><span class="font-medium">{{ row.name }}</span><p class="mt-1 text-xs text-gray-500">#{{ row.id }} · {{ row.plan || '未知套餐' }}</p></template>
    <template #cell-status="{ row }"><span class="text-sm" :class="row.rate_limited ? 'text-amber-600 dark:text-amber-400' : ''">{{ row.rate_limited ? '429 限流，跳过' : row.status === 'active' && row.schedulable ? '可用' : '不可用，跳过' }}</span></template>
    <template #cell-override="{ row }"><span class="text-xs text-gray-500">{{ row.override.mode === 'disabled' ? '已排除' : row.override.mode === 'rule' ? '单独指定' : '继承规则' }}</span></template>
    <template #cell-window_end="{ row }"><span class="text-xs text-gray-500">{{ time(row.window_end) }}</span></template>
   </DataTable>
   <WarmupRunTable v-else :items="runs" :loading="loading" />
  </template>
  <template #pagination><Pagination v-if="tab === 'runs'" v-model:page="page" v-model:page-size="pageSize" :total="total" /><p v-else class="text-xs text-gray-500">{{ tab === 'rules' ? state.rules.length + ' 条规则' : filteredAccounts.length + ' 个账号' }}</p></template>
 </TablePageLayout>
 <WarmupRuleDialog v-if="editing" :show="editing" :rule="draft" :accounts="state.accounts" :timezone="state.timezone" :saving="busy" :models="state.models" :groups="state.groups" @close="editing = false; draft = null" @save="onSave" />
 <BaseDialog :show="!!preview" title="热身执行预览" width="wide" @close="preview = null"><div v-if="preview" class="space-y-4 text-sm"><p>{{ previewRule?.name }} · 匹配 {{ preview.accounts.length }} 个账号 · 最多 {{ preview.max_requests }} 次请求</p><p class="text-gray-500">{{ preview.quota_check }}</p><div class="max-h-36 overflow-auto rounded-lg bg-gray-50 p-3 dark:bg-dark-800"><p v-for="a in preview.accounts" :key="a.id" class="py-1">{{ a.name }}<span v-if="a.rate_limited || !a.schedulable" class="ml-2 text-amber-600">执行时跳过</span></p></div><p class="font-medium">接下来计划时间</p><div class="text-xs text-gray-500"><p v-for="s in preview.next" :key="s" class="py-1">{{ time(s) }}</p><p v-if="!preview.next.length">未来 8 天无执行计划。</p></div><p class="text-xs text-gray-500">立即执行会发出真实请求并消耗账号额度，仍遵守总开关、账号策略及每日上限。</p></div><template #footer><button class="btn btn-secondary" @click="preview = null">关闭</button><button class="btn btn-primary" :disabled="busy || !state.settings.enabled || !previewRule?.enabled || !preview?.accounts.length" @click="run">确认立即执行</button></template></BaseDialog>
 <BaseDialog :show="!!deleteRule" title="删除热身规则" @close="deleteRule = null"><p>确认删除 {{ deleteRule?.name }}？待执行任务将取消，已有记录保留。</p><template #footer><button class="btn btn-secondary" @click="deleteRule = null">取消</button><button class="btn btn-primary" :disabled="busy" @click="remove">确认删除</button></template></BaseDialog>
</template>
