<script setup lang="ts">
import { checkinMoney } from "@/api/checkin";
import type { BlindboxStatus } from "@/api/blindbox";
defineProps<{ status: BlindboxStatus }>();
</script>

<template>
  <aside class="space-y-5">
    <section class="card p-5" aria-label="参与榜">
      <div class="mb-4 flex items-center justify-between gap-3">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">
          参与榜
        </h3>
        <small class="text-xs text-gray-500 dark:text-gray-400"
          >{{ status.participants }} 人 · {{ status.round.count }} 份</small
        >
      </div>
      <ol v-if="status.ranking.length" class="space-y-3.5">
        <li
          v-for="(item, index) in status.ranking"
          :key="index"
          class="flex items-center gap-3 text-sm"
        >
          <span class="w-4 shrink-0 text-xs tabular-nums text-gray-400">{{
            index + 1
          }}</span
          ><span
            class="min-w-0 flex-1 truncate text-gray-700 dark:text-gray-300"
            >{{ item.name
            }}<small v-if="item.mine" class="ml-1 text-blue-500"
              >我</small
            ></span
          ><b
            class="text-xs font-medium tabular-nums text-gray-700 dark:text-gray-300"
            >{{ item.count }} 份</b
          >
        </li>
      </ol>
      <p v-else class="py-6 text-center text-xs text-gray-400">还没有人参与</p>
      <p v-if="status.ranking.length === 20" class="mt-4 text-xs text-gray-400">
        展示前 20 名
      </p>
    </section>
    <section class="card p-5" aria-label="盈利中奖公示">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">
        中奖公示
      </h3>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
        {{ status.winners_date || "等待首场开奖"
        }}<span v-if="status.winners_date"> · 整场净赚</span>
      </p>
      <ol v-if="status.winners.length" class="mt-4 space-y-4">
        <li
          v-for="(item, index) in status.winners"
          :key="index"
          class="flex items-center justify-between gap-3 text-sm"
        >
          <span class="min-w-0 truncate text-gray-700 dark:text-gray-300"
            >{{ item.name
            }}<small v-if="item.mine" class="ml-1 text-blue-500">我</small
            ><small class="mt-1 block text-xs text-gray-400"
              >参与 {{ item.count }} 份</small
            ></span
          ><span
            class="shrink-0 text-right font-medium tabular-nums text-blue-600 dark:text-blue-400"
            >+{{ checkinMoney(item.profit || "0")
            }}<small class="mt-1 block text-xs font-normal text-gray-400"
              >到账 {{ checkinMoney(item.reward || "0") }}</small
            ></span
          >
        </li>
      </ol>
      <p v-else class="py-8 text-center text-xs text-gray-400">暂无盈利记录</p>
    </section>
  </aside>
</template>
