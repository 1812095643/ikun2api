<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, shallowRef, watch } from "vue";
import { checkinMoney } from "@/api/checkin";
import type { BlindboxStatus } from "@/api/blindbox";

const props = defineProps<{ status: BlindboxStatus; busy: boolean }>();
defineEmits<{ buy: [] }>();
const tick = shallowRef(0);
let timer: ReturnType<typeof setInterval> | undefined;
const observedAt = shallowRef(Date.now());
watch(
  () => props.status.server_time,
  () => {
    observedAt.value = Date.now();
  },
);
const remaining = computed(() => {
  void tick.value;
  const elapsed = Date.now() - observedAt.value;
  const seconds = Math.max(
    0,
    Math.floor(
      (Date.parse(props.status.round.draw_at) -
        Date.parse(props.status.server_time) -
        elapsed) /
        1000,
    ),
  );
  return `${String(Math.floor(seconds / 3600)).padStart(2, "0")}:${String(Math.floor(seconds / 60) % 60).padStart(2, "0")}:${String(seconds % 60).padStart(2, "0")}`;
});
const percent = (count: number) =>
  props.status.round.count
    ? `${((count * 100) / props.status.round.count).toFixed(1)}%`
    : "—";
onMounted(() => {
  timer = setInterval(() => {
    tick.value++;
  }, 1000);
});
onBeforeUnmount(() => clearInterval(timer));
</script>

<template>
  <section class="card blindbox-pool" aria-label="当前盲盒奖池">
    <header class="blindbox-pool-header">
      <div><h3>本场奖池</h3><p>{{ status.round.date }} · {{ status.round.spec.draw_time }} 开奖（北京时间）</p></div>
      <span class="blindbox-countdown">{{ status.round.status === 'settled' ? '已开奖' : remaining === '00:00:00' ? '等待结算' : remaining }}</span>
    </header>
    <dl class="blindbox-metrics">
      <div><dt>奖池金额</dt><dd>{{ checkinMoney(status.round.total_paid) }}</dd></div>
      <div><dt>参与人数</dt><dd>{{ status.participants }}<small>人</small></dd></div>
      <div><dt>售出份数</dt><dd>{{ status.round.count }}<small>份</small></dd></div>
    </dl>
    <div class="blindbox-purchase">
      <div><p>今日消费 <strong>{{ checkinMoney(status.eligibility.usage_amount) }}</strong></p><small>可购 {{ status.eligibility.limit }} · 已购 {{ status.eligibility.bought }} · 剩余 {{ status.eligibility.remaining }} 份</small></div>
      <button type="button" class="blindbox-buy btn btn-primary" :disabled="busy || !status.eligibility.can_buy || remaining === '00:00:00'" @click="$emit('buy')">{{ busy ? '处理中…' : status.eligibility.can_buy ? `${checkinMoney(status.round.spec.price)} / 份 · 参与` : status.eligibility.reason }}</button>
    </div>
    <div class="blindbox-probability-header"><h4>奖励与概率</h4><div><span>亏损 {{ percent(status.plan.lose) }}</span><span>保本 {{ percent(status.plan.even) }}</span><span class="text-blue-600 dark:text-blue-400">盈利 {{ percent(status.plan.win) }}</span></div></div>
    <div v-if="status.plan.tiers.length" class="blindbox-probability-table">
      <table><thead><tr><th>返还金额</th><th>份数</th><th>每份概率</th></tr></thead><tbody><tr v-for="tier in status.plan.tiers" :key="tier.amount"><td><strong>{{ checkinMoney(tier.amount) }}</strong><small :class="tier.result === 'win' ? 'text-blue-600 dark:text-blue-400' : 'text-gray-400'">{{ tier.result === 'win' ? '盈利' : tier.result === 'even' ? '保本' : '亏损' }}</small></td><td>{{ tier.count }}</td><td>{{ tier.probability }}%</td></tr></tbody></table>
    </div>
    <p v-else class="py-6 text-center text-xs text-gray-400">等待首份参与后生成奖池</p>
    <p class="blindbox-probability-note">概率随份数变化，以截止时公示为准；每份同概率随机分配。</p>
    <details class="blindbox-rules"><summary>参与规则</summary><ol>
      <li>当天完成签到且有已入账、金额大于 0 的模型消费。消费大于 0 至 100 可购 1 份，超过 100 至 200 可购 2 份，依此类推；盲盒支出不计入模型消费。</li>
      <li>每份 {{ checkinMoney(status.round.spec.price) }}，开奖前截止，以服务器时间为准；本场最多 10,000 份。</li>
      <li>购买余额全部组成奖池并返还。每份 {{ checkinMoney(status.round.spec.minimum) }} 至 {{ checkinMoney(status.round.spec.maximum) }}，目标 30% 保本，其余亏损或盈利，实际比例见公示；单份参与原额返还。</li>
      <li>开奖后自动到账，开盒仅展示结果。奖励不计入充值资格或邀请返利；同一笔购买重试不会重复扣款。</li>
      <li>首份购买后锁定本场规则；参与榜公示份数，中奖榜仅展示整场净赚用户，不公开邮箱。</li>
    </ol></details>
  </section>
</template>
<style scoped>
.blindbox-pool { padding: 20px; color: #111827; }
.dark .blindbox-pool { color: #f3f4f6; }
.blindbox-pool-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.blindbox-pool-header h3 { font-size: 14px; font-weight: 600; }
.blindbox-pool-header p { margin-top: 4px; color: #6b7280; font-size: 11px; }
.dark .blindbox-pool-header p { color: #94a3b8; }
.blindbox-countdown { padding: 5px 8px; border-radius: 6px; font-size: 12px; background: #f3f4f6; font-variant-numeric: tabular-nums; white-space: nowrap; }
.dark .blindbox-countdown { background: #0f172a; }
.blindbox-metrics { display: grid; grid-template-columns: repeat(3,minmax(0,1fr)); gap: 12px; padding: 18px 0; }
.blindbox-metrics dt { color: #6b7280; font-size: 11px; }
.dark .blindbox-metrics dt { color: #94a3b8; }
.blindbox-metrics dd { font-size: 22px; font-weight: 600; margin-top: 4px; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.blindbox-metrics small { font-size: 11px; font-weight: 400; margin-left: 4px; }
.blindbox-purchase { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 12px; padding: 12px; border-radius: 8px; background: #f9fafb; }
.dark .blindbox-purchase { background: #0f172a; }
.blindbox-purchase p { font-size: 12px; }
.blindbox-purchase small { display: block; margin-top: 4px; font-size: 11px; color: #6b7280; }
.dark .blindbox-purchase small { color: #94a3b8; }
.blindbox-probability-header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; margin: 18px 0 10px; }
.blindbox-probability-header h4 { font-size: 13px; font-weight: 600; }
.blindbox-probability-header > div { display: flex; gap: 10px; font-size: 11px; color: #6b7280; }
.dark .blindbox-probability-header > div { color: #94a3b8; }
.blindbox-probability-table { max-height: 220px; overflow: auto; border: 1px solid #e5e7eb; border-radius: 8px; }
.dark .blindbox-probability-table { border-color: #334155; }
.blindbox-probability-table table { width: 100%; font-size: 12px; text-align: left; font-variant-numeric: tabular-nums; }
.blindbox-probability-table thead { position: sticky; top: 0; background: #f9fafb; }
.dark .blindbox-probability-table thead { background: #1e293b; }
.blindbox-probability-table th { font-weight: 500; color: #6b7280; }
.dark .blindbox-probability-table th { color: #94a3b8; }
.blindbox-probability-table td,.blindbox-probability-table th { padding: 9px 12px; }
.blindbox-probability-table tr + tr { border-top: 1px solid #e5e7eb; }
.dark .blindbox-probability-table tr + tr { border-color: #334155; }
.blindbox-probability-table td:last-child,.blindbox-probability-table th:last-child { text-align: right; }
.blindbox-probability-table small { margin-left: 8px; font-size: 11px; }
.blindbox-probability-note { font-size: 11px; color: #6b7280; margin-top: 8px; line-height: 1.6; }
.dark .blindbox-probability-note { color: #94a3b8; }
.blindbox-rules { margin-top: 14px; padding-top: 12px; border-top: 1px solid #e5e7eb; font-size: 12px; }
.dark .blindbox-rules { border-color: #334155; }
.blindbox-rules summary { cursor: pointer; font-weight: 500; }
.blindbox-rules ol { list-style: decimal; padding-left: 18px; margin-top: 10px; color: #6b7280; font-size: 11px; line-height: 1.8; }
.dark .blindbox-rules ol { color: #94a3b8; }
.blindbox-rules li + li { margin-top: 5px; }
.blindbox-buy { @apply from-blue-500 to-blue-600 shadow-blue-500/20 hover:from-blue-600 hover:to-blue-700 focus:ring-blue-500/50; font-size: 12px; }
@media(max-width:640px) { .blindbox-pool { padding: 16px; } .blindbox-metrics dd { font-size: 20px; } }
</style>
