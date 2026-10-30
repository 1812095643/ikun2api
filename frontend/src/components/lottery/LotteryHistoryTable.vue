<script setup lang="ts">
import {
  lotteryMoney,
  participationNames,
  prizeNames,
  type LotteryParticipation,
  type LotteryWin
} from '@/api/lottery'
defineProps<{
  items: LotteryParticipation[]
  manage: boolean
  busy: number | null
  fixedActivity?: boolean
  loading: boolean
}>()
const emit = defineEmits<{
  detail: [id: number]
  claim: [win: LotteryWin]
  share: [win: LotteryWin]
}>()
function recordTime(value: string | null) {
  return value
    ? new Intl.DateTimeFormat('zh-CN', {
        timeZone: 'Asia/Shanghai',
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        hour12: false
      }).format(new Date(value))
    : '—'
}
function winAction(item: LotteryParticipation, action: 'claim' | 'share') {
  if (!item.win_id || !item.prize_key) return
  const win: LotteryWin = {
    id: item.win_id,
    lottery_id: item.lottery_id,
    title: item.title,
    user_id: item.user_id,
    user_email: item.email,
    user_name: item.username,
    prize_key: item.prize_key,
    amount: item.amount,
    created_at: item.drawn_at || item.joined_at,
    claimed_at: item.claimed_at,
    redeem_id: item.redeem_id || undefined
  }
  if (action === 'claim') emit('claim', win)
  else emit('share', win)
}
</script>
<template>
  <div class="lottery-history-table" :aria-busy="loading">
    <table>
      <thead>
        <tr>
          <th v-if="!fixedActivity">活动</th>
          <th v-if="manage">参与用户</th>
          <th>报名时间</th>
          <th>参与结果</th>
          <th>中奖奖项 / 金额</th>
          <th>开奖时间</th>
          <th>到账情况</th>
          <th v-if="!manage">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="item in items" :key="item.id">
          <td v-if="!fixedActivity" data-label="活动">
            <button class="lottery-text-button" @click="emit('detail', item.lottery_id)">
              {{ item.title }}</button
            ><small>活动 #{{ item.lottery_id }} · 报名 #{{ item.id }}</small>
          </td>
          <td v-if="manage" data-label="参与用户">
            <strong>{{ item.username || `用户 #${item.user_id}` }}</strong
            ><small>{{ item.email }}</small
            ><small>用户 #{{ item.user_id }}</small>
          </td>
          <td data-label="报名时间">{{ recordTime(item.joined_at) }}</td>
          <td data-label="参与结果">
            <span class="lottery-history-result" :data-result="item.result">{{
              participationNames[item.result]
            }}</span>
          </td>
          <td data-label="中奖奖项 / 金额">
            <template v-if="item.prize_key"
              ><span>{{ prizeNames[item.prize_key] }}</span
              ><strong class="lottery-history-amount"
                >${{ lotteryMoney(item.amount) }}</strong
              ></template
            ><span v-else class="lottery-muted">—</span>
          </td>
          <td data-label="开奖时间">{{ recordTime(item.drawn_at) }}</td>
          <td data-label="到账情况">
            <template v-if="item.win_id"
              ><span>{{ item.claimed_at ? '已到账' : '待领取' }}</span
              ><small v-if="item.claimed_at">{{ recordTime(item.claimed_at) }}</small
              ><small v-if="item.redeem_id">充值流水 #{{ item.redeem_id }}</small></template
            ><span v-else class="lottery-muted">—</span>
          </td>
          <td v-if="!manage" data-label="操作">
            <div class="lottery-history-actions">
              <button
                v-if="item.win_id && !item.claimed_at"
                class="btn btn-secondary lottery-button lottery-primary"
                :disabled="busy !== null"
                @click="winAction(item, 'claim')"
              >
                {{ busy === item.win_id ? '核验中' : '领取奖金' }}</button
              ><button
                v-if="item.win_id"
                class="btn btn-secondary lottery-button"
                @click="winAction(item, 'share')"
              >
                一键炫耀</button
              ><button v-else class="lottery-text-button" @click="emit('detail', item.lottery_id)">
                活动详情
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
