<script setup lang="ts">
import DataTable from "@/components/common/DataTable.vue";
import Pagination from "@/components/common/Pagination.vue";
import { checkinMoney, type CheckinStatus } from "@/api/checkin";

defineProps<{ status: CheckinStatus; loading: boolean; busy: boolean }>();
defineEmits<{ page: [value: number]; refresh: [] }>();

const columns = [
  { key: "day", label: "签到日期" },
  { key: "amount", label: "领取余额" },
  { key: "consecutive_days", label: "连续签到" },
  { key: "mode", label: "奖励方式" },
];
</script>

<template>
  <section class="space-y-4" aria-label="签到记录">
    <div class="flex items-center justify-between gap-4">
      <h2 class="text-base font-semibold text-gray-900 dark:text-gray-100">
        签到记录
      </h2>
      <button
        type="button"
        class="btn btn-secondary"
        :disabled="loading || busy"
        @click="$emit('refresh')"
      >
        刷新
      </button>
    </div>
    <DataTable :columns="columns" :data="status.items" :loading="loading">
      <template #cell-amount="{ value }">
        <span class="font-medium tabular-nums">+{{ checkinMoney(value) }}</span>
      </template>
      <template #cell-consecutive_days="{ value }">{{ value }} 天</template>
      <template #cell-mode="{ value }">{{
        value === "fixed" ? "固定金额" : "随机金额"
      }}</template>
      <template #empty>
        <p class="py-6 text-sm text-gray-500 dark:text-gray-400">
          还没有签到记录
        </p>
      </template>
    </DataTable>
    <Pagination
      v-if="status.total > status.page_size"
      :total="status.total"
      :page="status.page"
      :page-size="status.page_size"
      :show-page-size-selector="false"
      @update:page="$emit('page', $event)"
    />
  </section>
</template>
