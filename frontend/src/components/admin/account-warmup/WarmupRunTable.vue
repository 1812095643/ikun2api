<script setup lang="ts">
import { shallowRef } from 'vue'
import DataTable from '@/components/common/DataTable.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { warmupStatus, type WarmupRun } from '@/api/accountWarmup'
defineProps<{ items: WarmupRun[]; loading: boolean }>()
const detail = shallowRef<WarmupRun | null>(null)
const columns = [{ key: 'account_name', label: '账号 / 规则' }, { key: 'status', label: '结果' }, { key: 'quota', label: '主动额度检查' }, { key: 'window', label: '五小时窗口' }, { key: 'attempts', label: '请求 / Token' }, { key: 'planned_at', label: '计划时间' }, { key: 'actions', label: '操作' }]
const time = (value?: string) => value ? new Date(value).toLocaleString() : '—'
function confirmed(row: WarmupRun) { return row.snapshot?.results?.slice().reverse().find(r => r.window_confirmed) }
</script>
<template>
 <DataTable :columns="columns" :data="items" :loading="loading" row-key="id">
  <template #cell-account_name="{ row }"><div class="font-medium">{{ row.account_name }}</div><p class="mt-1 text-xs text-gray-500">{{ row.rule_name }}</p></template>
  <template #cell-status="{ row }"><span class="badge" :class="row.status === 'failed' || row.status === 'unknown' ? 'badge-warning' : 'badge-primary'">{{ warmupStatus[row.status] || row.status }}</span><p v-if="row.reason" class="mt-1 max-w-64 text-xs text-gray-500">{{ row.reason }}</p></template>
  <template #cell-quota="{ row }"><div v-if="row.snapshot?.before" class="text-xs">五小时 {{ row.snapshot.before.five_hour }}% · 周 {{ row.snapshot.before.seven_day }}%<p class="mt-1 text-gray-500">{{ time(row.snapshot.before.checked_at) }}</p></div><span v-else class="text-gray-400">尚未查询</span></template>
  <template #cell-window="{ row }"><span v-if="confirmed(row)" class="text-xs">{{ time(confirmed(row)?.window_reset) }} 重置</span><span v-else class="text-xs text-gray-500">{{ row.status === 'success' ? '请求成功，窗口待确认' : '未确认' }}</span></template>
  <template #cell-attempts="{ row }"><span class="text-xs tabular-nums">{{ row.attempts }} 次 · 输入 {{ row.input_tokens }} / 输出 {{ row.output_tokens }}</span></template>
  <template #cell-planned_at="{ row }"><span class="whitespace-nowrap text-xs">{{ time(row.planned_at) }}</span></template>
  <template #cell-actions="{ row }"><button class="btn btn-ghost btn-sm" @click="detail = row">详情</button></template>
 </DataTable>
 <BaseDialog :show="!!detail" title="热身执行详情" width="wide" @close="detail = null"><div v-if="detail" class="space-y-3 text-sm"><p>{{ detail.account_name }} · {{ detail.rule_name }}</p><p>{{ warmupStatus[detail.status] }} {{ detail.reason }}</p><p>主动查询：{{ time(detail.snapshot?.before?.checked_at) }}</p><div v-for="(result, index) in detail.snapshot?.results" :key="index" class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800">第 {{ index + 1 }} 条：{{ warmupStatus[result.status] || result.status }}<p class="mt-1 text-gray-500">{{ result.window_confirmed ? '上游确认窗口：' + time(result.window_reset) : '窗口未确认' }}</p></div><p class="text-xs text-gray-500">本页用量属于管理员运维消耗。</p></div></BaseDialog>
</template>
