<script setup lang="ts">
import { onMounted, shallowRef } from "vue";
import DataTable from "@/components/common/DataTable.vue";
import Pagination from "@/components/common/Pagination.vue";
import BlindboxRecords from "./BlindboxRecords.vue";
import {
  blindboxAPI,
  type BlindboxEntryPage,
  type BlindboxRoundPage,
} from "@/api/blindbox";
import { checkinMoney } from "@/api/checkin";
import { workspaceError } from "@/api/workspace";

const rounds = shallowRef<BlindboxRoundPage | null>(null);
const entries = shallowRef<BlindboxEntryPage | null>(null);
const selected = shallowRef<number>();
const page = shallowRef(1);
const loading = shallowRef(false);
const error = shallowRef("");
let revision = 0;
const columns = [
  { key: "date", label: "场次日期" },
  { key: "status", label: "状态" },
  { key: "count", label: "份数" },
  { key: "total_paid", label: "购买总额" },
  { key: "total_awarded", label: "发奖总额" },
  { key: "difference", label: "结算差额" },
  { key: "actions", label: "" },
];
async function load(nextPage = page.value) {
  loading.value = true;
  try {
    rounds.value = await blindboxAPI.rounds(nextPage);
    page.value = nextPage;
    error.value = "";
  } catch (e) {
    error.value = workspaceError(e);
  } finally {
    loading.value = false;
  }
}
async function detail(id: number, nextPage = 1) {
  const current = ++revision;
  selected.value = id;
  try {
    const result = await blindboxAPI.entries(nextPage, true, id);
    if (current === revision) entries.value = result;
  } catch (e) {
    error.value = workspaceError(e);
  }
}
onMounted(() => {
  void load();
});
</script>

<template>
  <section class="space-y-4">
    <div class="flex items-center justify-between gap-4">
      <h2 class="text-base font-semibold text-gray-900 dark:text-gray-100">
        盲盒场次与账目
      </h2>
      <button class="btn btn-secondary" :disabled="loading" @click="load()">
        刷新
      </button>
    </div>
    <p v-if="error" class="text-sm text-red-600 dark:text-red-400" role="alert">
      {{ error }}
    </p>
    <DataTable
      :columns="columns"
      :data="rounds?.items || []"
      :loading="loading"
    >
      <template #cell-status="{ value }">{{
        value === "settled" ? "已结算" : "报名中"
      }}</template>
      <template #cell-total_paid="{ value }">{{
        checkinMoney(value)
      }}</template>
      <template #cell-total_awarded="{ value }">{{
        checkinMoney(value)
      }}</template>
      <template #cell-difference="{ row }">{{
        row.status === "settled"
          ? checkinMoney(
              (Number(row.total_paid) - Number(row.total_awarded)).toFixed(2),
            )
          : "待结算"
      }}</template>
      <template #cell-actions="{ row }"
        ><button class="btn btn-secondary btn-sm" @click="detail(row.id)">
          参与明细
        </button></template
      >
      <template #empty
        ><p class="py-6 text-sm text-gray-500 dark:text-gray-400">
          暂无盲盒场次
        </p></template
      >
    </DataTable>
    <Pagination
      v-if="rounds && rounds.total > rounds.page_size"
      :total="rounds.total"
      :page="rounds.page"
      :page-size="rounds.page_size"
      :show-page-size-selector="false"
      @update:page="load"
    />
    <BlindboxRecords
      v-if="entries"
      :history="entries"
      :loading="false"
      admin
      @page="detail(selected!, $event)"
    />
  </section>
</template>
