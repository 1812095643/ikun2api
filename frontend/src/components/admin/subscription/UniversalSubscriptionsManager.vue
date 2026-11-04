<script setup lang="ts">
import { onMounted, ref, shallowRef, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import UniversalSubscriptionTable from './UniversalSubscriptionTable.vue'
import UniversalSubscriptionAdjustDialog from './UniversalSubscriptionAdjustDialog.vue'
import UniversalSubscriptionGrantDialog from './UniversalSubscriptionGrantDialog.vue'
import { useUniversalSubscriptions } from '@/composables/useUniversalSubscriptions'
import { universalSubscriptionsAPI, universalError, subscriptionMoney as money, subscriptionTime as time, type UniversalSubscription, type UniversalRecords } from '@/api/universalSubscriptions'
const { items, loading, total, page, userId, error, refresh } = useUniversalSubscriptions(true)
const app = useAppStore()
const route = useRoute()
const initialUser = typeof route.query.user_id === 'string' && /^\d+$/.test(route.query.user_id) ? route.query.user_id : ''
userId.value = initialUser ? Number(initialUser) : undefined
const showGrant = shallowRef(false); const filter = shallowRef(initialUser); const busy = shallowRef(false)
const editRow = ref<UniversalSubscription | null>(null)
const actionRow = ref<UniversalSubscription | null>(null); const targetStatus = shallowRef(''); const reason = shallowRef('')
const records = ref<UniversalRecords | null>(null); const recordsRow = ref<UniversalSubscription | null>(null)
function changeStatus(row: UniversalSubscription, status: string) { actionRow.value = row; targetStatus.value = status; reason.value = '' }
async function confirmStatus() { if (!actionRow.value || !reason.value.trim() || busy.value) return; busy.value = true; try { await universalSubscriptionsAPI.status(actionRow.value, targetStatus.value, reason.value); actionRow.value = null; await refresh() } catch (e) { app.showError(universalError(e, '操作失败')) } finally { busy.value = false } }
async function openRecords(row: UniversalSubscription) { try { records.value = await universalSubscriptionsAPI.records(row.id); recordsRow.value = row } catch { app.showError('记录读取失败') } }
async function resolve(id: string) { if (!reason.value.trim()) { app.showError('请填写已核对上游账单、确认未消费的依据'); return } busy.value = true; try { await universalSubscriptionsAPI.resolveHold(id, reason.value); if (recordsRow.value) await openRecords(recordsRow.value); await refresh(); reason.value = '' } catch (e) { app.showError(universalError(e, '操作失败')) } finally { busy.value = false } }
function applyFilter() { userId.value = /^\d+$/.test(filter.value.trim()) ? Number(filter.value) : undefined; page.value = 1; void refresh() }
function flip(delta: number) { page.value += delta; void refresh() }
watch(() => route.query.user_id, value => { filter.value = typeof value === 'string' ? value : ''; applyFilter() })
onMounted(refresh)
</script>
<template>
 <div class="space-y-4">
  <div class="flex flex-wrap items-center justify-between gap-3"><form class="flex gap-2" @submit.prevent="applyFilter"><input v-model="filter" aria-label="按用户 ID 筛选" placeholder="按用户 ID 筛选" class="input w-52" /><button class="btn btn-secondary">筛选</button></form><div class="flex gap-2"><button class="btn btn-secondary" :disabled="loading" @click="refresh">刷新</button><button class="btn btn-primary" @click="showGrant = true">发放订阅</button></div></div>
  <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
  <UniversalSubscriptionTable :items="items" :loading="loading" @records="openRecords" @edit="editRow = $event" @status="changeStatus" />
  <div class="flex items-center justify-between text-sm text-gray-500"><span>共 {{ total }} 份</span><div class="flex items-center gap-3"><button class="btn btn-secondary" :disabled="page <= 1 || loading" @click="flip(-1)">上一页</button><span>{{ page }}</span><button class="btn btn-secondary" :disabled="page * 30 >= total || loading" @click="flip(1)">下一页</button></div></div>
  <UniversalSubscriptionAdjustDialog :row="editRow" @close="editRow = null" @saved="refresh" />
  <UniversalSubscriptionGrantDialog :show="showGrant" @close="showGrant = false" @saved="refresh" />
  <BaseDialog :show="!!actionRow" title="修改订阅状态" @close="actionRow = null"><p class="mb-3 text-sm">{{ actionRow?.name }} · 用户 {{ actionRow?.user_id }}。停用后新请求会立即停止；已受理请求仍按实际用量结算。</p><label for="status-reason" class="input-label">操作原因</label><textarea id="status-reason" v-model="reason" rows="3" class="input" maxlength="4000" /><template #footer><button class="btn btn-secondary" :disabled="busy" @click="actionRow = null">取消</button><button class="btn btn-primary" :disabled="busy || !reason.trim()" @click="confirmStatus">确认</button></template></BaseDialog>
  <BaseDialog :show="!!recordsRow" :title="`${recordsRow?.name || ''} · 执行记录`" width="wide" @close="recordsRow = null">
   <div class="space-y-5"><section v-if="records?.holds.length"><h3 class="mb-2 font-medium">执行中与待核对预留</h3><p class="mb-3 text-xs leading-5 text-gray-500">上游受理情况不明确的请求保留额度，避免重复消费。请核对账单后再释放；仍在执行的请求不能释放。</p><div v-for="hold in records.holds" :key="hold.id" class="mb-2 flex flex-wrap items-center justify-between gap-2 rounded-lg border border-gray-200 p-3 text-xs dark:border-dark-600"><span>Key #{{ hold.api_key_id }} · {{ money(hold.amount) }} USD · {{ hold.state === 'reserved' ? '执行中' : '待核对' }}<span class="mt-1 block text-gray-500">{{ time(hold.created_at) }}</span></span><button v-if="hold.state === 'unknown'" class="text-primary-600" :disabled="busy" @click="resolve(hold.id)">确认未消费并释放</button></div><textarea v-model="reason" aria-label="预留核对依据" class="input mt-3" placeholder="填写账单核对依据" maxlength="4000" /></section>
    <section><h3 class="mb-2 font-medium">变更记录</h3><ol class="space-y-3"><li v-for="record in records?.events" :key="record.id" class="border-l-2 border-primary-200 pl-4 text-sm dark:border-primary-800"><div class="flex justify-between gap-3"><span>{{ record.action }}</span><time class="text-xs text-gray-500">{{ time(record.created_at) }}</time></div><p class="mt-1 break-words text-xs text-gray-500">{{ record.reason || '系统处理' }}</p></li></ol></section>
   </div>
  </BaseDialog>
 </div>
</template>
