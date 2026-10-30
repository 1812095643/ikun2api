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
  <section class="card overflow-hidden" aria-label="当前盲盒奖池">
    <div class="p-5 sm:p-6">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 class="text-lg font-semibold text-gray-900 dark:text-gray-100">
            每日盲盒
          </h3>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ status.round.date }} ·
            {{ status.round.spec.draw_time }} 开奖（北京时间）
          </p>
        </div>
        <span
          class="rounded-md bg-gray-100 px-2.5 py-1 text-xs text-gray-600 dark:bg-dark-800 dark:text-gray-300"
          >{{
            status.round.status === "settled"
              ? "已开奖"
              : remaining === "00:00:00"
                ? "等待结算"
                : `距开奖 ${remaining}`
          }}</span
        >
      </div>
      <dl class="my-6 grid grid-cols-3 gap-3">
        <div>
          <dt class="text-xs text-gray-500 dark:text-gray-400">当前奖池</dt>
          <dd
            class="mt-2 text-2xl font-semibold tabular-nums text-gray-900 dark:text-gray-100"
          >
            {{ checkinMoney(status.round.total_paid) }}
          </dd>
        </div>
        <div>
          <dt class="text-xs text-gray-500 dark:text-gray-400">参与人数</dt>
          <dd
            class="mt-2 text-2xl font-semibold tabular-nums text-gray-900 dark:text-gray-100"
          >
            {{ status.participants
            }}<small class="ml-1 text-xs font-normal">人</small>
          </dd>
        </div>
        <div>
          <dt class="text-xs text-gray-500 dark:text-gray-400">售出份数</dt>
          <dd
            class="mt-2 text-2xl font-semibold tabular-nums text-gray-900 dark:text-gray-100"
          >
            {{ status.round.count
            }}<small class="ml-1 text-xs font-normal">份</small>
          </dd>
        </div>
      </dl>
      <div
        class="flex flex-wrap items-center justify-between gap-4 rounded-lg bg-gray-50 p-4 dark:bg-dark-800/60"
      >
        <div class="text-sm text-gray-700 dark:text-gray-300">
          <p>
            今日消费
            <b class="font-medium tabular-nums">{{
              checkinMoney(status.eligibility.usage_amount)
            }}</b>
          </p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            可购 {{ status.eligibility.limit }} 份 · 已购
            {{ status.eligibility.bought }} 份 · 剩余
            {{ status.eligibility.remaining }} 份
          </p>
        </div>
        <button
          type="button"
          class="blindbox-buy btn btn-primary"
          :disabled="
            busy || !status.eligibility.can_buy || remaining === '00:00:00'
          "
          @click="$emit('buy')"
        >
          {{
            status.eligibility.can_buy
              ? `支付 ${checkinMoney(status.round.spec.price)} 参与`
              : status.eligibility.reason
          }}
        </button>
      </div>
      <div class="mt-6">
        <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
          <h4 class="text-sm font-semibold text-gray-800 dark:text-gray-200">
            奖池与概率公示
          </h4>
          <span class="text-xs text-gray-500 dark:text-gray-400"
            >亏损 {{ percent(status.plan.lose) }} · 保本
            {{ percent(status.plan.even) }} · 盈利
            {{ percent(status.plan.win) }}</span
          >
        </div>
        <div
          v-if="status.plan.tiers.length"
          class="max-h-64 overflow-y-auto rounded-lg border border-gray-100 dark:border-dark-700"
        >
          <table class="w-full text-left text-sm tabular-nums">
            <thead
              class="sticky top-0 bg-gray-50 text-xs text-gray-500 dark:bg-dark-800 dark:text-gray-400"
            >
              <tr>
                <th class="px-4 py-2.5 font-medium">返还金额</th>
                <th class="px-4 py-2.5 font-medium">份数</th>
                <th class="px-4 py-2.5 text-right font-medium">每份概率</th>
              </tr>
            </thead>
            <tbody
              class="divide-y divide-gray-100 text-gray-700 dark:divide-dark-700 dark:text-gray-300"
            >
              <tr v-for="tier in status.plan.tiers" :key="tier.amount">
                <td class="px-4 py-2.5">
                  {{ checkinMoney(tier.amount)
                  }}<small
                    class="ml-2 text-xs"
                    :class="
                      tier.result === 'win'
                        ? 'text-blue-600 dark:text-blue-400'
                        : 'text-gray-400'
                    "
                    >{{
                      tier.result === "win"
                        ? "盈利"
                        : tier.result === "even"
                          ? "保本"
                          : "亏损"
                    }}</small
                  >
                </td>
                <td class="px-4 py-2.5">{{ tier.count }}</td>
                <td class="px-4 py-2.5 text-right">{{ tier.probability }}%</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p
          v-else
          class="rounded-lg bg-gray-50 px-4 py-5 text-center text-sm text-gray-500 dark:bg-dark-800/50 dark:text-gray-400"
        >
          等待首份参与后生成奖池
        </p>
        <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-gray-400">
          概率随售出份数变化，以截止时清单为准；每份同概率随机分配。单份参与原额返还。
        </p>
      </div>
      <details
        class="mt-5 border-t border-gray-100 pt-4 text-sm dark:border-dark-700"
        open
      >
        <summary
          class="cursor-pointer font-medium text-gray-800 dark:text-gray-200"
        >
          参与规则
        </summary>
        <ol
          class="mt-3 list-decimal space-y-2 pl-5 text-xs leading-5 text-gray-500 dark:text-gray-400"
        >
          <li>
            当天完成签到，且有已入账、金额大于 0 的模型使用记录。消费大于 0 至
            100 可购 1 份，超过 100 至 200 可购 2
            份，依此类推。购买盲盒不算模型消费。
          </li>
          <li>
            每次支付 {{ checkinMoney(status.round.spec.price) }} 购买 1
            份；开奖前截止，服务器时间为准，本场最多 10,000 份。
          </li>
          <li>
            全部购买余额组成奖池，全部返还。每份奖励在
            {{ checkinMoney(status.round.spec.minimum) }} 至
            {{ checkinMoney(status.round.spec.maximum) }} 内，目标 30%
            保本，其余亏损或盈利；人数取整或极窄区间会有微调，实际比例见上表。
          </li>
          <li>
            开奖后自动到账，点击开盒只展示结果。奖励不计入充值资格或邀请返利；断线重试同一笔购买不会重复扣款。
          </li>
          <li>
            首份购买后锁定本场规则。参与榜公示份数；中奖榜仅展示整场净赚用户，不公开邮箱。
          </li>
        </ol>
      </details>
    </div>
  </section>
</template>

<style scoped>
.blindbox-buy {
  @apply from-blue-500 to-blue-600 shadow-blue-500/20 hover:from-blue-600 hover:to-blue-700 hover:shadow-blue-500/20 focus:ring-blue-500/50;
}
</style>
