<script setup lang="ts">
import { reactive, shallowRef, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { universalSubscriptionsAPI, universalError, type UniversalSubscription } from '@/api/universalSubscriptions'
const props = defineProps<{ row: UniversalSubscription | null }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const busy = shallowRef(false); const error = shallowRef('')
const form = reactive({ total_credit_usd: 0, daily_limit_usd: 0, weekly_limit_usd: 0, monthly_limit_usd: 0, expires_at: '', reason: '' })
const fields = [{ key: 'total_credit_usd', label: '套餐总额度' }, { key: 'daily_limit_usd', label: '日上限（0 不限）' }, { key: 'weekly_limit_usd', label: '周上限（0 不限）' }, { key: 'monthly_limit_usd', label: '月上限（0 不限）' }] as const
watch(() => props.row, row => { if (!row) return; const d = new Date(row.expires_at); d.setMinutes(d.getMinutes() - d.getTimezoneOffset()); Object.assign(form, { total_credit_usd: row.total_credit_usd || 0, daily_limit_usd: row.daily_limit_usd || 0, weekly_limit_usd: row.weekly_limit_usd || 0, monthly_limit_usd: row.monthly_limit_usd || 0, expires_at: d.toISOString().slice(0, 16), reason: '' }); error.value = '' }, { immediate: true })
async function save() { if (!props.row || !form.reason.trim() || busy.value) return; busy.value = true; try { await universalSubscriptionsAPI.adjust(props.row, { ...form, expires_at: new Date(form.expires_at).toISOString() }); emit('saved'); emit('close') } catch (e) { error.value = universalError(e, '调整失败') } finally { busy.value = false } }
</script>
<template>
 <BaseDialog :show="!!row" title="调整通用订阅" :show-close-button="!busy" @close="emit('close')"><form id="subscription-adjust" class="space-y-4" @submit.prevent="save"><p class="text-sm text-gray-500">调整仅影响此用户的这一期权益，不修改销售套餐，不重置已用额度。</p><div class="grid gap-4 sm:grid-cols-2"><div v-for="field in fields" :key="field.key"><label :for="`adjust-${field.key}`" class="input-label">{{ field.label }}（USD）</label><input :id="`adjust-${field.key}`" v-model.number="form[field.key]" type="number" min="0" step="0.01" class="input" required /></div></div><div><label for="adjust-expiry" class="input-label">到期时间（当前浏览器时区）</label><input id="adjust-expiry" v-model="form.expires_at" type="datetime-local" class="input" required /></div><div><label for="adjust-reason" class="input-label">调整原因</label><textarea id="adjust-reason" v-model="form.reason" rows="2" class="input" maxlength="4000" required /></div><p class="text-xs text-gray-500">新额度不能低于已消费和预留金额；有效期不能与已排期订阅重叠。</p><p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p></form><template #footer><button class="btn btn-secondary" :disabled="busy" @click="emit('close')">取消</button><button type="submit" form="subscription-adjust" class="btn btn-primary" :disabled="busy || !form.reason.trim()">保存调整</button></template></BaseDialog>
</template>
