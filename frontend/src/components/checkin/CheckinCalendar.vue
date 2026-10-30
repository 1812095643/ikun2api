<script setup lang="ts">
import { computed, shallowRef } from "vue";
import Icon from "@/components/icons/Icon.vue";
import { checkinMoney, type CheckinRecord } from "@/api/checkin";

const props = defineProps<{
  month: string;
  today: string;
  items: CheckinRecord[];
  loading: boolean;
}>();
const emit = defineEmits<{ month: [value: string] }>();
const selected = shallowRef("");
const records = computed(
  () => new Map(props.items.map((item) => [item.day, item])),
);
const monthTitle = computed(
  () =>
    `${Number(props.month.slice(0, 4))} 年 ${Number(props.month.slice(5))} 月`,
);
const cells = computed(() => {
  const [year, month] = props.month.split("-").map(Number);
  const first = new Date(Date.UTC(year, month - 1, 1));
  const offset = (first.getUTCDay() + 6) % 7;
  const count = new Date(Date.UTC(year, month, 0)).getUTCDate();
  return Array.from(
    { length: Math.ceil((offset + count) / 7) * 7 },
    (_, index) => {
      const number = index - offset + 1;
      const day = `${props.month}-${String(number).padStart(2, "0")}`;
      return {
        number,
        day,
        outside: number < 1 || number > count,
        record: records.value.get(day),
      };
    },
  );
});
const detail = computed(() => {
  if (!selected.value.startsWith(props.month))
    return `本月已签到 ${props.items.length} 天`;
  const item = records.value.get(selected.value);
  return `${selected.value} · ${item ? `领取 ${checkinMoney(item.amount)}` : selected.value > props.today ? "尚未到来" : "未签到"}`;
});
function changeMonth(delta: number) {
  const [year, month] = props.month.split("-").map(Number);
  const date = new Date(Date.UTC(year, month - 1 + delta, 1));
  emit("month", date.toISOString().slice(0, 7));
  selected.value = "";
}
</script>

<template>
  <section class="checkin-calendar" aria-label="签到日历" :aria-busy="loading">
    <header class="mb-4 flex items-center justify-between gap-3">
      <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-200">
        {{ monthTitle }}
      </h3>
      <div class="flex gap-1">
        <button
          type="button"
          class="calendar-nav"
          aria-label="上个月"
          :disabled="loading"
          @click="changeMonth(-1)"
        >
          <Icon name="chevronLeft" size="sm" />
        </button>
        <button
          type="button"
          class="calendar-nav"
          aria-label="下个月"
          :disabled="loading || month >= today.slice(0, 7)"
          @click="changeMonth(1)"
        >
          <Icon name="chevronRight" size="sm" />
        </button>
      </div>
    </header>
    <div class="calendar-grid">
      <span
        v-for="weekday in ['一', '二', '三', '四', '五', '六', '日']"
        :key="weekday"
        class="mb-1 text-center text-[10px] text-gray-400"
        >{{ weekday }}</span
      >
      <template v-for="(cell, index) in cells" :key="index">
        <span v-if="cell.outside" />
        <button
          v-else
          type="button"
          class="calendar-day"
          :class="{
            signed: cell.record,
            today: cell.day === today,
            future: cell.day > today,
            selected: cell.day === selected,
          }"
          :aria-label="`${cell.day} ${cell.record ? `已签到，领取 ${checkinMoney(cell.record.amount)}` : '未签到'}`"
          :aria-current="cell.day === today ? 'date' : undefined"
          :title="
            cell.record
              ? `${cell.day} +${checkinMoney(cell.record.amount)}`
              : cell.day
          "
          @click="selected = cell.day"
        >
          {{ cell.number }}
        </button>
      </template>
    </div>
    <footer
      class="mt-4 flex flex-wrap items-center justify-between gap-2 text-xs text-gray-500 dark:text-gray-400"
    >
      <span aria-live="polite">{{ detail }}</span>
      <span class="inline-flex items-center gap-1.5"
        ><i class="h-2.5 w-2.5 rounded-sm bg-green-500" />已签到</span
      >
    </footer>
  </section>
</template>

<style scoped>
.checkin-calendar {
  width: 100%;
  max-width: 310px;
}
.calendar-grid {
  display: grid;
  grid-template-columns: repeat(7, minmax(0, 1fr));
  gap: 6px;
}
.calendar-day {
  @apply flex aspect-square items-center justify-center rounded-md bg-gray-100 text-xs font-medium tabular-nums text-gray-500 transition-colors hover:bg-gray-200 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:bg-dark-800 dark:text-gray-400 dark:hover:bg-dark-700;
}
.calendar-day.signed {
  @apply bg-green-500 text-white hover:bg-green-600 dark:bg-green-700 dark:text-green-50 dark:hover:bg-green-600;
}
.calendar-day.today {
  @apply ring-1 ring-blue-500 ring-offset-2 dark:ring-offset-dark-900;
}
.calendar-day.future {
  @apply opacity-40;
}
.calendar-day.selected {
  @apply outline outline-2 outline-offset-2 outline-gray-400;
}
.calendar-nav {
  @apply rounded-md p-1.5 text-gray-500 hover:bg-gray-100 disabled:opacity-25 dark:text-gray-400 dark:hover:bg-dark-800;
}
@media (max-width: 640px) {
  .checkin-calendar {
    max-width: none;
  }
  .calendar-day {
    max-height: 38px;
    aspect-ratio: auto;
    height: 34px;
  }
}
@media (prefers-reduced-motion: reduce) {
  .calendar-day {
    transition: none;
  }
}
</style>
