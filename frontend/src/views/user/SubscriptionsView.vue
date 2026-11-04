<script setup lang="ts">
import { onMounted } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import UniversalSubscriptionCard from '@/components/payment/UniversalSubscriptionCard.vue'
import { useUniversalSubscriptions } from '@/composables/useUniversalSubscriptions'
const { items, loading, error, total, page, refresh } = useUniversalSubscriptions()
function flip(n: number) { page.value += n; void refresh() }
onMounted(refresh)
</script>
<template>
 <AppLayout><div class="mx-auto max-w-5xl space-y-5"><header class="flex flex-wrap items-center justify-between gap-3"><p class="text-sm text-gray-500 dark:text-gray-400">查看通用订阅的额度、周期用量与有效期。</p><div class="flex gap-2"><button class="btn btn-secondary" :disabled="loading" @click="refresh">刷新</button><RouterLink :to="{ path: '/purchase', query: { tab: 'subscription' } }" class="btn btn-primary">购买 / 续购订阅</RouterLink></div></header><p class="rounded-xl bg-primary-50 px-4 py-3 text-sm leading-6 text-primary-800 dark:bg-primary-950/30 dark:text-primary-200">创建或编辑 API Key 时，将计费来源选为「通用订阅」。所有订阅 Key 跨分组共同累计；每个分组按自己的实际倍率扣费。</p><p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p><p v-if="loading && !items.length" class="card p-12 text-center text-gray-500">正在读取订阅…</p><div v-else-if="!items.length" class="card space-y-3 p-12 text-center"><h2 class="font-semibold">还没有通用订阅</h2><p class="text-sm text-gray-500">购买套餐或兑换订阅码后，权益会显示在这里。</p><RouterLink :to="{ path: '/purchase', query: { tab: 'subscription' } }" class="btn btn-primary inline-flex">查看套餐</RouterLink></div><div class="space-y-4"><UniversalSubscriptionCard v-for="item in items" :key="item.id" :subscription="item" /></div><p class="text-xs leading-5 text-gray-500">日额度按站点时区零点刷新，周按 7 天、月按 30 天滚动。周期重置不会恢复套餐总额度。续购排在已有订阅结束后生效。</p><div v-if="total > 30" class="flex justify-end gap-3"><button class="btn btn-secondary" :disabled="page <= 1 || loading" @click="flip(-1)">上一页</button><button class="btn btn-secondary" :disabled="page * 30 >= total || loading" @click="flip(1)">下一页</button></div></div></AppLayout>
</template>
