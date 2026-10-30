<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import { lotteryMoney, lotteryTime, prizeNames, type LotteryWin } from '@/api/lottery'
defineProps<{ items: LotteryWin[]; busy: number | null }>()
defineEmits<{ claim: [win: LotteryWin]; share: [win: LotteryWin]; detail: [id: number] }>()
</script>
<template>
  <div v-if="!items.length" class="lottery-empty">
    <p>暂无中奖记录</p>
  </div>
  <div v-else class="lottery-records">
    <article v-for="win in items" :key="win.id" class="lottery-record">
      <div class="lottery-record-mark" aria-hidden="true">
        {{ win.prize_key === 'special' ? '✳' : '✧' }}
      </div>
      <div class="lottery-record-info">
        <span class="lottery-kicker"
          >{{ prizeNames[win.prize_key] }} · {{ lotteryTime(win.created_at) }}</span
        ><button class="lottery-text-button" @click="$emit('detail', win.lottery_id)">
          {{ win.title }}</button
        ><small class="lottery-muted">{{
          win.claimed_at
            ? `${lotteryTime(win.claimed_at)} 已存入余额 · 流水 #${win.redeem_id}`
            : '奖金待领取'
        }}</small>
      </div>
      <strong class="lottery-record-amount">+ ${{ lotteryMoney(win.amount) }}</strong>
      <div class="lottery-record-actions">
        <button
          v-if="!win.claimed_at"
          class="btn btn-secondary lottery-button lottery-primary"
          :disabled="busy !== null"
          @click="$emit('claim', win)"
        >
          {{ busy === win.id ? '核验并入账中' : '领取奖金' }}</button
        ><button class="btn btn-secondary lottery-button" @click="$emit('share', win)">
          <Icon name="externalLink" size="sm" />一键炫耀
        </button>
      </div>
    </article>
  </div>
</template>
