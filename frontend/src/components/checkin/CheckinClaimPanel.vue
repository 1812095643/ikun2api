<script setup lang="ts">
import { computed } from "vue";
import Icon from "@/components/icons/Icon.vue";
import CheckinCalendar from "./CheckinCalendar.vue";
import { checkinMoney, type CheckinStatus } from "@/api/checkin";

const props = defineProps<{
  status: CheckinStatus;
  busy: boolean;
  loading: boolean;
}>();
defineEmits<{ claim: []; month: [value: string] }>();

const reward = computed(() => {
  const s = props.status.settings;
  if (props.status.checked_in)
    return `今日已领取 ${checkinMoney(props.status.today_amount)}`;
  if (!s.enabled) return "签到暂未开放";
  return s.mode === "fixed"
    ? `签到领取 ${checkinMoney(s.fixed_amount)}`
    : `随机领取 ${checkinMoney(s.random_min)} ～ ${checkinMoney(s.random_max)}`;
});
</script>

<template>
  <section class="card p-5 sm:p-6" aria-label="每日签到">
    <div class="grid items-start gap-7 md:grid-cols-[1fr_310px] lg:gap-10">
      <div>
        <div class="flex flex-wrap items-start justify-between gap-5">
          <div class="space-y-2">
            <p class="text-sm text-gray-500 dark:text-gray-400">
              {{ status.today }}
            </p>
            <h2 class="text-xl font-semibold text-gray-900 dark:text-gray-100">
              {{ reward }}
            </h2>
            <p class="text-xs text-gray-500 dark:text-gray-400">
              每日 00:00 重置（北京时间）
            </p>
          </div>
        </div>
        <button
          type="button"
          class="checkin-claim btn btn-primary mt-5 min-w-32"
          :disabled="busy || status.checked_in || !status.settings.enabled"
          :aria-busy="busy"
          @click="$emit('claim')"
        >
          <Icon v-if="status.checked_in" name="check" size="sm" />
          {{ busy ? "领取中…" : status.checked_in ? "今日已签到" : "立即签到" }}
        </button>
        <dl
          class="mt-6 grid grid-cols-3 gap-3 border-t border-gray-100 pt-5 dark:border-dark-700"
        >
          <div>
            <dt class="text-xs text-gray-500 sm:text-sm dark:text-gray-400">
              累计签到
            </dt>
            <dd
              class="mt-2 text-xl font-semibold tabular-nums text-gray-900 dark:text-gray-100"
            >
              {{ status.summary.total_days }}
              <span class="text-xs font-normal">天</span>
            </dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 sm:text-sm dark:text-gray-400">
              连续签到
            </dt>
            <dd
              class="mt-2 text-xl font-semibold tabular-nums text-gray-900 dark:text-gray-100"
            >
              {{ status.summary.consecutive_days }}
              <span class="text-xs font-normal">天</span>
            </dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 sm:text-sm dark:text-gray-400">
              累计领取
            </dt>
            <dd
              class="mt-2 break-all text-xl font-semibold tabular-nums text-gray-900 dark:text-gray-100"
            >
              {{ checkinMoney(status.summary.total_amount) }}
            </dd>
          </div>
        </dl>
      </div>
      <CheckinCalendar
        :month="status.month"
        :today="status.today"
        :items="status.month_items"
        :loading="loading"
        @month="$emit('month', $event)"
      />
    </div>
  </section>
</template>

<style scoped>
.checkin-claim {
  @apply from-blue-500 to-blue-600 shadow-blue-500/20 hover:from-blue-600 hover:to-blue-700 hover:shadow-blue-500/20 focus:ring-blue-500/50 dark:shadow-blue-500/15;
}
@media (prefers-reduced-motion: reduce) {
  .checkin-claim {
    transition: none;
    transform: none;
  }
}
</style>
