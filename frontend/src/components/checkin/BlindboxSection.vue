<script setup lang="ts">
import { computed, watch } from "vue";
import BaseDialog from "@/components/common/BaseDialog.vue";
import BlindboxPool from "./BlindboxPool.vue";
import BlindboxLeaderboard from "./BlindboxLeaderboard.vue";
import BlindboxRecords from "./BlindboxRecords.vue";
import { useBlindbox } from "@/composables/workspace/useBlindbox";
import { checkinMoney } from "@/api/checkin";

const props = defineProps<{ checkinRevision: string }>();
const {
  status,
  history,
  loading,
  busy,
  error,
  message,
  confirming,
  confirmationPrice,
  opening,
  revealed,
  load,
  confirmBuy,
  buy,
  open,
} = useBlindbox();
watch(
  () => props.checkinRevision,
  () => {
    void load();
  },
);
const profit = computed(() =>
  revealed.value?.reward === null || !revealed.value
    ? 0
    : Number(revealed.value.reward) - Number(revealed.value.paid),
);
</script>

<template>
  <section
    class="space-y-5 border-t border-gray-200 pt-7 dark:border-dark-700"
    aria-label="每日盲盒活动"
  >
    <div class="flex items-center justify-between gap-3">
      <h2 class="text-base font-semibold text-gray-900 dark:text-gray-100">
        每日盲盒
      </h2>
      <button
        class="btn btn-secondary"
        type="button"
        :disabled="loading || busy"
        @click="load()"
      >
        刷新
      </button>
    </div>
    <p
      v-if="error"
      class="rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300"
      role="alert"
    >
      {{ error }}
    </p>
    <p
      v-if="message"
      class="rounded-lg bg-blue-50 px-4 py-3 text-sm text-blue-700 dark:bg-blue-950/40 dark:text-blue-300"
      role="status"
    >
      {{ message }}
    </p>
    <div
      v-if="status"
      class="grid items-start gap-5 xl:grid-cols-[minmax(0,1fr)_280px]"
    >
      <div class="min-w-0 space-y-6">
        <BlindboxPool :status="status" :busy="busy" @buy="confirmBuy" />
        <BlindboxRecords
          v-if="history"
          :history="history"
          :loading="loading"
          :opening="opening"
          @page="load"
          @open="open"
        />
      </div>
      <BlindboxLeaderboard :status="status" />
    </div>
    <p
      v-else-if="loading"
      class="py-8 text-center text-sm text-gray-500 dark:text-gray-400"
    >
      正在读取奖池…
    </p>
    <BaseDialog
      :show="confirming"
      title="确认参与"
      width="narrow"
      @close="!busy && (confirming = false)"
    >
      <p class="text-sm leading-7 text-gray-700 dark:text-gray-300">
        支付 <strong>{{ checkinMoney(confirmationPrice) }}</strong> 余额购买 1
        份盲盒。奖励可能低于购买金额，截止时按公示奖池随机分配。
      </p>
      <p
        v-if="error"
        class="mt-3 text-sm text-red-600 dark:text-red-400"
        role="alert"
      >
        {{ error }}
      </p>
      <template #footer
        ><button
          type="button"
          class="btn btn-secondary"
          :disabled="busy"
          @click="confirming = false"
        >
          取消</button
        ><button
          type="button"
          class="blindbox-confirm btn btn-primary"
          :disabled="busy"
          @click="buy"
        >
          {{ busy ? "支付中…" : `确认支付 ${checkinMoney(confirmationPrice)}` }}
        </button></template
      >
    </BaseDialog>
    <BaseDialog
      :show="revealed !== null"
      title="盲盒已开启"
      width="narrow"
      @close="revealed = null"
    >
      <div v-if="revealed" class="py-5 text-center">
        <p class="text-sm text-gray-500 dark:text-gray-400">
          {{ revealed.date }} · 第 {{ revealed.ordinal }} 份
        </p>
        <p
          class="my-5 text-4xl font-semibold tabular-nums text-gray-900 dark:text-gray-100"
        >
          {{ checkinMoney(revealed.reward || "0") }}
        </p>
        <p class="text-sm text-gray-600 dark:text-gray-300">
          {{
            profit > 0
              ? `净赚 ${checkinMoney(profit.toFixed(2))}`
              : profit < 0
                ? `本次亏损 ${checkinMoney((-profit).toFixed(2))}`
                : "本次保本"
          }}
          · 已自动到账
        </p>
      </div>
      <template #footer
        ><button
          type="button"
          class="btn btn-secondary"
          @click="revealed = null"
        >
          收下好运
        </button></template
      >
    </BaseDialog>
  </section>
</template>

<style scoped>
.blindbox-confirm {
  @apply from-blue-500 to-blue-600 shadow-blue-500/20 hover:from-blue-600 hover:to-blue-700 hover:shadow-blue-500/20;
}
</style>
