<script setup lang="ts">
import { computed } from 'vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import UpstreamBalanceDisplayMenu from './UpstreamBalanceDisplayMenu.vue'
import type { BalanceColumn, BalanceSort, UpstreamBalanceDisplay } from '@/api/upstreamBalances'
const props = defineProps<{
  search: string; filter: string; visibility: string; display: UpstreamBalanceDisplay
  total: number; hiddenCount: number; selectedCount: number; resultCount: number
  counts: { low: number; error: number; models: number; monitored: number }
  loading: boolean; querying: boolean; modelBusy: boolean; displaySaving: boolean; ready: boolean
}>()
const emit = defineEmits<{
  'update:search': [value: string]; 'update:filter': [value: string]; 'update:visibility': [value: string]
  sort: [value: BalanceSort]; toggleColumn: [key: BalanceColumn]; compact: []; resetDisplay: []
  reload: []; refresh: []; models: []; querySelected: []; hideSelected: []; showSelected: []; clearSelection: []
}>()
const tabs = computed(() => [ { key: 'visible', label: '显示中', count: props.total - props.hiddenCount }, { key: 'hidden', label: '已隐藏', count: props.hiddenCount }, { key: 'all', label: '全部', count: props.total } ])
const filters = computed(() => [ { value: 'all', label: '全部状态' }, { value: 'low', label: `余额偏低 · ${props.counts.low}` }, { value: 'error', label: `查询异常 · ${props.counts.error}` }, { value: 'models', label: `模型有变更 · ${props.counts.models}` }, { value: 'monitored', label: `已启用监控 · ${props.counts.monitored}` } ])
const sorts = [{ value: 'manual', label: '手动排序' }, { value: 'name', label: '名称 A–Z' }, { value: 'balance_asc', label: '余额从低到高' }, { value: 'balance_desc', label: '余额从高到低' }, { value: 'recent', label: '最近查询优先' }]
</script>

<template>
  <div class="space-y-3">
    <div class="flex flex-wrap items-center justify-between gap-x-4 gap-y-3">
      <div class="flex items-center gap-1 rounded-xl bg-gray-100/80 p-1 dark:bg-dark-800" role="group" aria-label="上游可见性">
        <button v-for="tab in tabs" :key="tab.key" type="button" :aria-pressed="visibility === tab.key" class="flex items-center gap-2 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500" :class="visibility === tab.key ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white' : 'text-gray-500 hover:text-gray-900 dark:text-dark-400 dark:hover:text-gray-200'" @click="emit('update:visibility', tab.key)"><span>{{ tab.label }}</span><span class="text-xs tabular-nums opacity-60">{{ tab.count }}</span></button>
      </div>
      <div class="flex items-center gap-2 text-xs text-gray-400 dark:text-dark-400"><span v-if="displaySaving" role="status" class="inline-flex items-center gap-1"><Icon name="refresh" size="xs" class="animate-spin" />保存中</span><span v-else>自动刷新</span><span>·</span><span>{{ total }} 个上游</span></div>
    </div>
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex w-full min-w-0 flex-wrap items-center gap-2 md:w-auto md:flex-1">
        <div class="relative w-full sm:w-auto sm:min-w-48 sm:flex-1 sm:max-w-72"><Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" /><input :value="search" aria-label="搜索供应商或地址" placeholder="搜索名称、地址、账号…" class="input pl-9 pr-9" @input="emit('update:search', ($event.target as HTMLInputElement).value)" /><button v-if="search" type="button" class="absolute right-2 top-1/2 -translate-y-1/2 rounded p-1 text-gray-400 hover:text-gray-700 dark:hover:text-white" aria-label="清除搜索" @click="emit('update:search', '')"><Icon name="x" size="sm" /></button></div>
        <Select :model-value="filter" :options="filters" class="min-w-0 flex-1 sm:w-40 sm:flex-none" aria-label="筛选上游" @update:model-value="emit('update:filter', String($event))" />
        <Select :model-value="display.sort_by" :options="sorts" class="min-w-0 flex-1 sm:w-40 sm:flex-none" aria-label="上游排序" @update:model-value="emit('sort', $event as BalanceSort)" />
      </div>
      <div class="flex w-full flex-wrap items-center gap-2 sm:w-auto">
        <button type="button" class="btn btn-secondary px-2.5" title="刷新列表" aria-label="刷新列表" :disabled="loading" @click="emit('reload')"><Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" /></button>
        <UpstreamBalanceDisplayMenu :display="display" :disabled="!ready" @toggle="emit('toggleColumn', $event)" @compact="emit('compact')" @reset="emit('resetDisplay')" />
        <button type="button" class="btn btn-secondary" :disabled="loading || modelBusy || !total" @click="emit('models')">{{ modelBusy ? '提交中…' : '检测模型' }}</button>
        <button type="button" class="btn btn-primary gap-1.5" :disabled="loading || querying || !resultCount" @click="emit('refresh')"><Icon name="sync" size="sm" />{{ querying ? '提交中…' : '查询余额' }}</button>
      </div>
    </div>
    <div v-if="selectedCount" role="region" aria-label="批量操作" class="flex flex-wrap items-center gap-2 rounded-xl bg-primary-50 px-3 py-2 text-sm dark:bg-primary-900/15">
      <span class="mr-2 font-medium text-primary-700 dark:text-primary-300">已选 {{ selectedCount }} 项</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="querying" @click="emit('querySelected')">查询所选</button>
      <button v-if="visibility !== 'hidden'" type="button" class="btn btn-ghost btn-sm" @click="emit('hideSelected')">隐藏所选</button>
      <button v-if="visibility !== 'visible'" type="button" class="btn btn-ghost btn-sm" @click="emit('showSelected')">显示所选</button>
      <button type="button" class="ml-auto rounded-lg p-1.5 text-gray-400 hover:bg-white/60 dark:hover:bg-dark-700" aria-label="取消选择" @click="emit('clearSelection')"><Icon name="x" size="sm" /></button>
    </div>
  </div>
</template>
