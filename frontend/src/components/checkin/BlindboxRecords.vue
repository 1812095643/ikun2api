<script setup lang="ts">
import { computed } from "vue";
import DataTable from "@/components/common/DataTable.vue";
import Pagination from "@/components/common/Pagination.vue";
import Icon from "@/components/icons/Icon.vue";
import { checkinMoney } from "@/api/checkin";
import type { BlindboxEntry, BlindboxEntryPage } from "@/api/blindbox";

const props = withDefaults(
  defineProps<{
    history: BlindboxEntryPage;
    loading: boolean;
    opening?: number | null;
    admin?: boolean;
  }>(),
  { opening: null, admin: false },
);
defineEmits<{ page: [value: number]; open: [value: BlindboxEntry] }>();
const columns = computed(() => [
  ...(props.admin ? [{ key: "username", label: "用户" }] : []),
  { key: "date", label: "场次日期" },
  { key: "ordinal", label: "份数" },
  { key: "paid", label: "支付余额" },
  { key: "reward", label: "返还余额" },
  { key: "result", label: "净收益" },
  ...(!props.admin ? [{ key: "actions", label: "" }] : []),
]);
function profit(entry: BlindboxEntry) {
  if (entry.reward === null) return "—";
  const value = Number(entry.reward) - Number(entry.paid);
  return `${value > 0 ? "+" : value < 0 ? "−" : ""}${checkinMoney(Math.abs(value).toFixed(2))}`;
}
</script>

<template>
  <section class="space-y-3" aria-label="盲盒参与记录">
    <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">
      {{ admin ? "全部参与记录" : "我的盲盒" }}
    </h3>
    <DataTable :columns="columns" :data="history.items" :loading="loading">
      <template #cell-ordinal="{ value }">第 {{ value }} 份</template>
      <template #cell-paid="{ value }">{{ checkinMoney(value) }}</template>
      <template #cell-reward="{ row }">{{
        row.reward === null
          ? "等待开奖"
          : admin || row.opened_at
            ? checkinMoney(row.reward)
            : "已到账 · 待开盒"
      }}</template>
      <template #cell-result="{ row }">{{
        admin || row.opened_at ? profit(row) : "—"
      }}</template>
      <template #cell-actions="{ row }">
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          :class="{ 'box-opening': opening === row.id }"
          :disabled="row.reward === null || opening !== null"
          @click="$emit('open', row)"
        >
          <Icon name="gift" size="sm" />{{
            opening === row.id ? "开启中…" : row.opened_at ? "查看" : "开盲盒"
          }}
        </button>
      </template>
      <template #empty
        ><p class="py-6 text-sm text-gray-500 dark:text-gray-400">
          还没有盲盒参与记录
        </p></template
      >
    </DataTable>
    <Pagination
      v-if="history.total > history.page_size"
      :total="history.total"
      :page="history.page"
      :page-size="history.page_size"
      :show-page-size-selector="false"
      @update:page="$emit('page', $event)"
    />
  </section>
</template>

<style scoped>
.box-opening :deep(svg) {
  animation: box-open 380ms ease-out both;
}
@keyframes box-open {
  35% {
    transform: translateY(-3px) rotate(-8deg);
  }
  70% {
    transform: translateY(-3px) rotate(8deg);
  }
}
@media (prefers-reduced-motion: reduce) {
  .box-opening :deep(svg) {
    animation: none;
  }
}
</style>
