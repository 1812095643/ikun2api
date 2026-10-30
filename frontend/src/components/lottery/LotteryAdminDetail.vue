<script setup lang="ts">
import { ref } from 'vue'
import LotteryDetail from './LotteryDetail.vue'
import LotteryHistory from './LotteryHistory.vue'
import { lotteryTime, type LotteryActivity } from '@/api/lottery'
defineProps<{ item: LotteryActivity; busy: boolean }>()
defineEmits<{ cancel: []; publish: [] }>()
const confirm = ref(false)
const eventNames: Record<string, string> = {
  save: '保存规则',
  publish: '发布活动',
  cancel: '取消活动',
  join: '用户报名',
  draw: '系统开奖',
  claim: '奖金入账'
}
</script>
<template>
  <div class="lottery-admin-detail">
    <LotteryDetail :item="item" />
    <section v-if="item.status === 'draft' || item.status === 'open'" class="lottery-admin-actions">
      <label class="lottery-confirm-label"
        ><input v-model="confirm" type="checkbox" />确认{{
          item.status === 'draft' ? '按当前规则发布' : '取消本场活动'
        }}</label
      ><button
        v-if="item.status === 'draft'"
        class="btn btn-secondary lottery-button lottery-primary"
        :disabled="busy || !confirm"
        @click="$emit('publish')"
      >
        发布活动</button
      ><button
        v-if="item.status === 'open'"
        class="btn btn-secondary lottery-button lottery-danger"
        :disabled="busy || !confirm"
        @click="$emit('cancel')"
      >
        取消活动
      </button>
    </section>
    <section class="lottery-admin-entries">
      <h3>本场参与与中奖情况</h3>
      <LotteryHistory manage :lottery-id="item.id" />
    </section>
    <details class="lottery-audit">
      <summary>操作审计与开奖凭证</summary>
      <p v-if="item.result_digest" class="lottery-digest">结果摘要 {{ item.result_digest }}</p>
      <ol>
        <li v-for="event in item.events" :key="event.id">
          <span>{{ lotteryTime(event.created_at) }}</span
          ><strong>{{ eventNames[event.action] || event.action }}</strong
          ><small>{{ event.actor_id ? `操作人 #${event.actor_id}` : '系统' }}</small>
        </li>
      </ol>
      <p class="lottery-muted">最多展示最近 200 条操作，完整记录保存在服务器。</p>
    </details>
  </div>
</template>
