<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import UpstreamBalanceTable from './UpstreamBalanceTable.vue'
import UpstreamBalanceEditor from './UpstreamBalanceEditor.vue'
import UpstreamBalanceDetails from './UpstreamBalanceDetails.vue'
import { useUpstreamBalances } from '@/composables/useUpstreamBalances'
import type { UpstreamBalance, UpstreamBalanceDraft } from '@/api/upstreamBalances'

const { items, loading, error, search, filter, page, pageSize, refreshing, saving, filtered, visible, busy, load, refresh, save } = useUpstreamBalances()
const editing = shallowRef<UpstreamBalance | null>(null)
const detailID = shallowRef('')
const details = computed(() => items.value.find(item => item.id === detailID.value) || null)
const filterOptions = [{ value: 'all', label: '全部上游' }, { value: 'monitored', label: '已启用监控' }, { value: 'low', label: '余额偏低' }, { value: 'error', label: '查询异常' }]
async function handleSave(id: string, draft: UpstreamBalanceDraft) { if (await save(id, draft)) editing.value = null }
</script>

<template>
  <TablePageLayout>
    <template #filters>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex flex-1 flex-wrap items-center gap-3"><div class="relative w-full sm:w-64"><Icon name="search" size="md" class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" /><input v-model="search" aria-label="搜索供应商或地址" placeholder="搜索供应商、URL、账号" class="input pl-10" /></div><Select v-model="filter" class="w-40" :options="filterOptions" aria-label="筛选上游" /><span class="text-xs text-gray-500 dark:text-dark-400">{{ items.length }} 个上游<span v-if="busy" class="ml-2 text-primary-600 dark:text-primary-400">查询进行中</span></span></div>
        <div class="flex shrink-0 items-center gap-3"><button type="button" class="btn btn-secondary" :disabled="loading" @click="load()">刷新列表</button><button type="button" class="btn btn-primary" :disabled="loading || !!refreshing || !items.length || busy" @click="refresh()">{{ refreshing === 'all' ? '提交中…' : '全部查询' }}</button></div>
      </div>
      <div v-if="error" role="alert" class="mt-3 flex items-center justify-between rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300"><span>{{ error }}</span><button class="underline" @click="load()">重试</button></div>
    </template>
    <template #table><UpstreamBalanceTable :items="visible" :loading="loading" :refreshing="refreshing" @edit="editing = $event" @details="detailID = $event.id" @refresh="refresh" /></template>
    <template #pagination><Pagination v-model:page="page" v-model:page-size="pageSize" :total="filtered.length" :show-page-size-selector="true" /></template>
  </TablePageLayout>
  <UpstreamBalanceEditor :item="editing" :saving="saving" @close="editing = null" @save="handleSave" />
  <UpstreamBalanceDetails :item="details" @close="detailID = ''" />
</template>
