<script setup lang="ts">
import { lotteryDrawText, lotteryMoney, statusNames, type LotteryActivity } from '@/api/lottery'
import LotteryPrizeList from './LotteryPrizeList.vue'
import LotteryRules from './LotteryRules.vue'
defineProps<{ item: LotteryActivity }>()
</script>
<template>
  <section class="lottery-overview" aria-label="活动概况">
    <div class="lottery-overview-facts">
      <span class="lottery-status" :data-status="item.status">{{ statusNames[item.status] }}</span>
      <span>奖池 <strong>${{ lotteryMoney(item.prize_total) }}</strong></span>
      <span>报名 <strong>{{ item.entry_count }}<small v-if="item.spec.capacity"> / {{ item.spec.capacity }}</small></strong> 人</span>
      <span>名额 <strong>{{ item.prize_count }}</strong> 位</span>
    </div>
    <LotteryPrizeList :prizes="item.spec.prizes" />
    <div class="lottery-overview-schedule"><span>{{ lotteryDrawText(item.spec) }}</span><small>北京时间</small></div>
    <LotteryRules :spec="item.spec" />
    <p v-if="item.spec.description" class="lottery-overview-description">{{ item.spec.description }}</p>
  </section>
</template>
