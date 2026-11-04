<script setup lang="ts">
import { shallowRef } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import UniversalSubscriptionsManager from '@/components/admin/subscription/UniversalSubscriptionsManager.vue'
import AdminPaymentPlansView from './orders/AdminPaymentPlansView.vue'
import adminAPI from '@/api/admin'
import { ref } from 'vue'
const route = useRoute()
const tab = shallowRef<'plans' | 'users' | 'legacy'>(route.query.user_id ? 'users' : 'plans')
const legacy = ref<Array<{ id: number; user_id: number; group_id: number; status: string; expires_at: string | null }>>([])
async function showLegacy() { tab.value = 'legacy'; try { legacy.value = (await adminAPI.subscriptions.list(1, 100)).items } catch { legacy.value = [] } }
</script>
<template>
 <AppLayout>
  <div class="space-y-5">
   <p class="text-sm leading-6 text-gray-500 dark:text-gray-400">管理全站通用的限时额度。套餐总额度与日、周、月限额共同生效，支持在线购买和管理员发放。</p>
   <div class="flex flex-wrap gap-1 rounded-xl bg-gray-100 p-1 dark:bg-dark-800" role="tablist" aria-label="订阅管理">
    <button role="tab" :aria-selected="tab === 'plans'" class="rounded-lg px-4 py-2 text-sm" :class="tab === 'plans' ? 'bg-white text-primary-600 shadow-sm dark:bg-dark-700' : 'text-gray-500'" @click="tab = 'plans'">套餐档位</button>
    <button role="tab" :aria-selected="tab === 'users'" class="rounded-lg px-4 py-2 text-sm" :class="tab === 'users' ? 'bg-white text-primary-600 shadow-sm dark:bg-dark-700' : 'text-gray-500'" @click="tab = 'users'">用户订阅与记录</button>
    <button role="tab" :aria-selected="tab === 'legacy'" class="rounded-lg px-4 py-2 text-sm" :class="tab === 'legacy' ? 'bg-white text-primary-600 shadow-sm dark:bg-dark-700' : 'text-gray-500'" @click="showLegacy">旧订阅存档</button>
   </div>
   <AdminPaymentPlansView v-if="tab === 'plans'" embedded />
   <UniversalSubscriptionsManager v-else-if="tab === 'users'" />
   <section v-else class="card overflow-x-auto p-5"><p class="mb-4 text-sm text-gray-500">旧分组订阅已停用，保留历史记录，不再销售、发放或参与扣费。</p><table class="w-full text-left text-sm"><thead><tr class="border-b dark:border-dark-600"><th class="p-2">编号</th><th>用户</th><th>分组</th><th>原状态</th><th>原到期时间</th></tr></thead><tbody><tr v-for="row in legacy" :key="row.id" class="border-b dark:border-dark-700"><td class="p-2">{{ row.id }}</td><td>{{ row.user_id }}</td><td>{{ row.group_id }}</td><td>{{ row.status }}</td><td>{{ row.expires_at ? new Date(row.expires_at).toLocaleString() : '—' }}</td></tr></tbody></table><p v-if="!legacy.length" class="py-8 text-center text-gray-500">暂无旧订阅记录</p></section>
  </div>
 </AppLayout>
</template>
