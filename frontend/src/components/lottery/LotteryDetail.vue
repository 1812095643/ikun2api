<script setup lang="ts">
import {
  lotteryDrawText,
  lotteryMoney,
  lotteryTime,
  prizeNames,
  type LotteryActivity,
  type LotteryWin
} from '@/api/lottery'
import LotteryPrizeList from './LotteryPrizeList.vue'
import LotteryRules from './LotteryRules.vue'
defineProps<{ item: LotteryActivity; busy?: boolean }>()
defineEmits<{ claim: [win: LotteryWin]; share: [win: LotteryWin] }>()
</script>
<template>
  <div class="lottery-detail">
    <p v-if="item.spec.description" class="lottery-description">{{ item.spec.description }}</p>
    <LotteryPrizeList :prizes="item.spec.prizes" />
    <LotteryRules :spec="item.spec" />
    <div class="lottery-detail-facts">
      <span>{{ item.entry_count }} 人报名 · {{ item.prize_count }} 个中奖名额</span
      ><span>{{ lotteryDrawText(item.spec) }} · 北京时间</span>
    </div>
    <Transition name="lottery-reveal" mode="out-in">
      <div
        v-if="item.status === 'drawn'"
        key="drawn"
        class="lottery-result"
        :class="{ 'lottery-result-win': item.my_win }"
        aria-live="polite"
      >
        <template v-if="item.my_win">
          <h3>
            {{ prizeNames[item.my_win.prize_key] }}
            <span>${{ lotteryMoney(item.my_win.amount) }}</span>
          </h3>
          <span class="lottery-muted">{{
            item.my_win.claimed_at ? '奖金已存入余额' : '核验后直接存入你的余额'
          }}</span>
          <div class="lottery-actions">
            <button
              v-if="!item.my_win.claimed_at"
              class="btn btn-secondary lottery-button lottery-primary"
              :disabled="busy"
              @click="$emit('claim', item.my_win)"
            >
              {{ busy ? '核验并入账中' : '领取奖金' }}</button
            ><button class="btn btn-secondary lottery-button" @click="$emit('share', item.my_win)">
              一键炫耀
            </button>
          </div></template
        >
        <template v-else
          ><h3>{{ item.joined ? '未中奖' : '已开奖' }}</h3></template
        >
      </div>
      <div v-else-if="item.joined" key="joined" class="lottery-joined-note">已报名，等待开奖</div>
    </Transition>
    <div v-if="item.status === 'drawn'" class="lottery-winner-list">
      <h3>
        中奖名单 <small>{{ lotteryTime(item.drawn_at) }}</small>
      </h3>
      <p v-if="!item.winners?.length" class="lottery-muted">本场没有符合开奖条件的报名用户。</p>
      <div v-for="(winner, i) in item.winners" :key="winner.id || i">
        <span>{{ prizeNames[winner.prize_key] }}</span
        ><span>{{ winner.user_name || '幸运用户' }}</span
        ><strong>${{ lotteryMoney(winner.amount) }}</strong>
      </div>
      <p v-if="(item.winners?.length || 0) >= 100" class="lottery-muted">
        展示前 100 位，个人结果以中奖记录为准。
      </p>
    </div>
  </div>
</template>
