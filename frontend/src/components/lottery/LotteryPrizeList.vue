<script setup lang="ts">
import { lotteryMoney, prizeNames, type LotteryPrize } from '@/api/lottery'
defineProps<{ prizes: LotteryPrize[] }>()
</script>
<template>
  <div class="lottery-prizes">
    <div
      v-for="(prize, index) in prizes"
      :key="prize.key"
      class="lottery-prize"
      :class="{ 'lottery-prize-leading': index === 0 }"
    >
      <span class="lottery-prize-name">{{ prizeNames[prize.key] }}</span>
      <strong><small>$</small>{{ lotteryMoney(prize.amount) }}</strong>
      <span class="lottery-muted">{{ prize.count }} 位</span>
    </div>
  </div>
</template>
