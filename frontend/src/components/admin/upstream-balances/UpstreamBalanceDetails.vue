<script setup lang="ts">
import BaseDialog from '@/components/common/BaseDialog.vue'
import { balanceComparisonLabel, balanceScopeLabel, formatBalanceTime, formatUpstreamBalance, type UpstreamBalance } from '@/api/upstreamBalances'
defineProps<{ item: UpstreamBalance | null }>()
const emit = defineEmits<{ close: [] }>()
</script>

<template>
  <BaseDialog :show="!!item" :title="`${item?.name || ''} · Key 明细`" width="wide" @close="emit('close')">
    <template v-if="item">
      <div class="mb-4 flex flex-wrap items-center justify-between gap-2 text-sm"><a :href="item.url" target="_blank" rel="noopener noreferrer" class="break-all text-primary-600 hover:underline dark:text-primary-400">{{ item.url }}</a><span class="text-xs text-gray-500 dark:text-dark-400">{{ formatBalanceTime(item.last_checked_at) }}</span></div>
      <div v-if="item.snapshot.error" role="status" class="mb-4 rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">{{ item.snapshot.error }}</div>
      <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-600">
        <table class="w-full text-left text-sm"><thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-800 dark:text-dark-300"><tr><th class="px-4 py-3 font-medium">来源账号</th><th class="px-4 py-3 font-medium">余额 / 额度</th><th class="px-4 py-3 font-medium">口径</th><th class="px-4 py-3 font-medium">状态</th></tr></thead><tbody class="divide-y divide-gray-100 dark:divide-dark-700">
          <tr v-for="(reading, index) in item.snapshot.readings" :key="index"><td class="px-4 py-3"><div v-for="(id, accountIndex) in reading.account_ids" :key="id" class="mb-1 text-gray-800 dark:text-dark-100">#{{ id }} {{ reading.account_names[accountIndex] }}<span v-if="id === item.config.preferred_account_id" class="ml-2 text-xs text-primary-600 dark:text-primary-400">监控来源</span></div></td><td class="whitespace-nowrap px-4 py-3 font-medium tabular-nums text-gray-900 dark:text-gray-100">{{ formatUpstreamBalance(reading.balance, reading.unit, reading.unlimited) }}</td><td class="whitespace-nowrap px-4 py-3 text-gray-500 dark:text-dark-300">{{ balanceScopeLabel(reading.scope) }}<div v-if="reading.wallet_user_id" class="mt-1 text-xs">上游用户 #{{ reading.wallet_user_id }}</div></td><td class="max-w-xs px-4 py-3"><span v-if="reading.error" class="text-xs text-amber-600 dark:text-amber-400">{{ reading.error }}</span><span v-else class="text-xs text-gray-600 dark:text-dark-300">查询成功</span></td></tr>
          <tr v-if="!item.snapshot.readings.length"><td colspan="4" class="px-4 py-10 text-center text-gray-500 dark:text-dark-400">{{ item.checking || item.queued ? '余额正在查询，请稍候。' : '尚无查询记录。' }}</td></tr>
        </tbody></table>
      </div>
      <div class="mt-4 flex flex-wrap items-center justify-between gap-2 text-xs text-gray-500 dark:text-dark-400"><span>{{ item.last_checked_at ? balanceComparisonLabel(item.snapshot.comparison) : '未校验' }}<span v-if="item.snapshot.comparison === 'consistent'">（误差不超过 0.01，仅比较读数）</span></span><span>相同 Key 只请求一次，不叠加余额。</span></div>
      <p v-if="item.token_configured" class="mt-2 text-xs text-gray-500 dark:text-dark-400">当前使用独立钱包令牌，模型 Key 的额度需在上游查看。</p>
      <p v-if="item.alert.error" class="mt-3 text-sm text-amber-600 dark:text-amber-400">{{ item.alert.error }}</p>
      <p v-if="item.alert.last_sent" class="mt-3 text-xs text-gray-500 dark:text-dark-400">最近余额提醒：{{ formatBalanceTime(item.alert.last_sent) }}</p>
    </template>
    <template #footer><button type="button" class="btn btn-secondary" @click="emit('close')">关闭</button></template>
  </BaseDialog>
</template>
