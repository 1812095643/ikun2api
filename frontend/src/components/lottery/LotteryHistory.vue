<script setup lang="ts">
import { lotteryMoney, type LotteryWin } from '@/api/lottery'
import { useLotteryHistory } from '@/composables/workspace/useLotteryHistory'
import LotteryHistoryFilters from './LotteryHistoryFilters.vue'
import LotteryHistoryTable from './LotteryHistoryTable.vue'
const props = withDefaults(
  defineProps<{ manage?: boolean; lotteryId?: number; revision?: number; busy?: number | null }>(),
  { manage: false, busy: null }
)
defineEmits<{ detail: [id: number]; claim: [win: LotteryWin]; share: [win: LotteryWin] }>()
const { data, loading, error, page, pages, result, activity, search, load, apply, changePage } =
  useLotteryHistory(props)
</script>
<template>
  <section class="lottery-history">
    <div v-if="data" class="lottery-history-summary" aria-label="参与情况汇总">
      <span
        >报名 <strong>{{ data.summary.entries }}</strong> 次</span
      ><span
        >待开奖 <strong>{{ data.summary.pending }}</strong></span
      ><span
        >中奖 <strong>{{ data.summary.won }}</strong> 次</span
      ><span
        >未中奖 <strong>{{ data.summary.lost }}</strong> 次</span
      ><span
        >中奖总额 <strong>${{ lotteryMoney(data.summary.amount) }}</strong></span
      ><span
        >已到账 <strong>${{ lotteryMoney(data.summary.credited_amount) }}</strong></span
      >
    </div>
    <LotteryHistoryFilters
      v-model:result="result"
      v-model:activity="activity"
      v-model:search="search"
      :manage="manage"
      :fixed-activity="!!lotteryId"
      :loading="loading"
      @apply="apply"
      @refresh="load"
    />
    <p v-if="error" class="lottery-error" role="alert">
      {{ error }} <button @click="load">重试</button>
    </p>
    <p class="lottery-history-count">{{ data?.total || 0 }} 条记录 <span>时间均为北京时间</span></p>
    <div v-if="!data?.items.length" class="lottery-empty">
      <p>{{ loading ? '加载中…' : '暂无参与记录' }}</p>
    </div>
    <LotteryHistoryTable
      v-else
      :items="data.items"
      :manage="manage"
      :fixed-activity="!!lotteryId"
      :busy="busy"
      :loading="loading"
      @detail="$emit('detail', $event)"
      @claim="$emit('claim', $event)"
      @share="$emit('share', $event)"
    />
    <div v-if="pages > 1" class="lottery-pagination">
      <button
        class="btn btn-secondary lottery-button"
        :disabled="loading || page <= 1"
        @click="changePage(-1)"
      >
        上一页</button
      ><span>{{ page }} / {{ pages }}</span
      ><button
        class="btn btn-secondary lottery-button"
        :disabled="loading || page >= pages"
        @click="changePage(1)"
      >
        下一页
      </button>
    </div>
  </section>
</template>
