<script setup lang="ts">
import { ref, shallowRef, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import adminAPI from '@/api/admin'
import { adminPaymentAPI } from '@/api/admin/payment'
import { universalSubscriptionsAPI, universalError } from '@/api/universalSubscriptions'
import type { SubscriptionPlan } from '@/types/payment'
const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const plans = ref<SubscriptionPlan[]>([])
const users = ref<Array<{ value: number; label: string }>>([])
const query = shallowRef(''); const userId = shallowRef<number | null>(null); const planId = shallowRef<number | null>(null)
const notes = shallowRef(''); const error = shallowRef(''); const busy = shallowRef(false); const searching = shallowRef(false)
let operationId = ''; let sequence = 0
watch(() => props.show, async show => { if (!show) return; operationId = crypto.randomUUID(); userId.value = null; planId.value = null; notes.value = ''; query.value = ''; error.value = ''; try { plans.value = (await adminPaymentAPI.getPlans()).data } catch { error.value = '无法读取套餐，请稍后重试' } })
async function search() {
 const token = ++sequence; searching.value = true
 try { const term = query.value.trim(); const data = /^\d+$/.test(term) ? { items: [await adminAPI.users.getById(Number(term))] } : await adminAPI.users.list(1, 20, { search: term }); if (token === sequence) users.value = data.items.map(x => ({ value: x.id, label: `${x.email} · ID ${x.id}` })) }
 catch { error.value = '用户查询失败' } finally { if (token === sequence) searching.value = false }
}
async function save() {
 if (!userId.value || !planId.value || busy.value) return
 busy.value = true; error.value = ''
 try { await universalSubscriptionsAPI.grant({ user_id: userId.value, plan_id: planId.value, operation_id: operationId, notes: notes.value }); emit('saved'); emit('close') }
 catch (e) { error.value = universalError(e, '发放失败，请重试') } finally { busy.value = false }
}
</script>
<template>
 <BaseDialog :show="show" title="发放通用订阅" :show-close-button="!busy" @close="emit('close')">
  <form id="universal-grant" class="space-y-4" @submit.prevent="save">
   <div><label for="grant-user-query" class="input-label">查找用户（邮箱、用户名或 ID）</label><div class="flex gap-2"><input id="grant-user-query" v-model="query" class="input" @keydown.enter.prevent="search" /><button type="button" class="btn btn-secondary" :disabled="searching" @click="search">查找</button></div></div>
   <Select v-model="userId" :options="users" placeholder="选择用户" searchable />
   <div><label class="input-label">套餐档位</label><Select v-model="planId" :options="plans.map(p => ({ value: p.id, label: `${p.name} · ${p.validity_days} 天` }))" placeholder="选择套餐" /></div>
   <div><label for="grant-notes" class="input-label">发放备注</label><textarea id="grant-notes" v-model="notes" class="input" rows="2" maxlength="4000" /></div>
   <p class="text-xs leading-5 text-gray-500">已有订阅时，新的一期自动排到现有订阅结束后。发放会保存当前套餐规则，不充值到普通余额。</p>
   <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
  </form>
  <template #footer><button class="btn btn-secondary" :disabled="busy" @click="emit('close')">取消</button><button type="submit" form="universal-grant" class="btn btn-primary" :disabled="busy || !userId || !planId">{{ busy ? '发放中…' : '确认发放' }}</button></template>
 </BaseDialog>
</template>
