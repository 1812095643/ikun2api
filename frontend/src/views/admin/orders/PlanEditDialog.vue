<script setup lang="ts">
import { computed, reactive, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatPaymentAmount } from '@/components/payment/currency'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { adminPaymentAPI, type AdminPaymentConfig } from '@/api/admin/payment'
import type { SubscriptionPlan } from '@/types/payment'
import type { AdminGroup } from '@/types'
const props = defineProps<{ show: boolean; plan: SubscriptionPlan | null; groups?: AdminGroup[]; paymentConfig?: AdminPaymentConfig | null }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const submitting = shallowRef(false)
const error = shallowRef('')
const form = reactive({ name: '', description: '', price: 0, original_price: 0, currency: '', validity_days: 30, total_credit_usd: 100, daily_limit_usd: 0, weekly_limit_usd: 0, monthly_limit_usd: 0, features: '', product_name: '', sort_order: 0, for_sale: false })
const subscriptionCnyPreview = computed(() => {
 const price = Number(form.price) || 0; const rate = Number(props.paymentConfig?.subscription_usd_to_cny_rate) || 0
 if (price <= 0 || rate <= 0) return null
 const amount = Math.round(price * rate * 100) / 100; const feeRate = Number(props.paymentConfig?.recharge_fee_rate) || 0
 const fee = feeRate > 0 ? Math.ceil(amount * feeRate) / 100 : 0
 const total = Math.round((amount + fee) * 100) / 100
 return { amount: formatPaymentAmount(amount, 'CNY'), feeRate, total: formatPaymentAmount(total, 'CNY') }
})
const limits = [{ key: 'daily_limit_usd', label: '每日上限' }, { key: 'weekly_limit_usd', label: '每周上限' }, { key: 'monthly_limit_usd', label: '每月上限' }] as const
watch(() => props.show, show => {
 if (!show) return
 error.value = ''; const p = props.plan
 Object.assign(form, { name: p?.name || '', description: p?.description || '', price: p?.price || 0, original_price: p?.original_price || 0, currency: p?.currency || '', validity_days: p?.validity_days || 30, total_credit_usd: p?.total_credit_usd || 100, daily_limit_usd: p?.daily_limit_usd || 0, weekly_limit_usd: p?.weekly_limit_usd || 0, monthly_limit_usd: p?.monthly_limit_usd || 0, features: Array.isArray(p?.features) ? p.features.join('\n') : p?.features || '', product_name: (p as (SubscriptionPlan & { product_name?: string }) | null)?.product_name || '', sort_order: p?.sort_order || 0, for_sale: p?.for_sale || false })
}, { immediate: true })
async function save() {
 if (submitting.value) return
 if (!form.name.trim() || form.price <= 0 || form.total_credit_usd <= 0 || !Number.isInteger(form.validity_days) || form.validity_days < 1) { error.value = '请填写名称、售价、有效天数和总额度'; return }
 submitting.value = true; error.value = ''
 try { const data = { ...form, name: form.name.trim(), validity_unit: 'day', plan_type: 'universal_credit' }; if (props.plan) await adminPaymentAPI.updatePlan(props.plan.id, data); else await adminPaymentAPI.createPlan(data); useAppStore().showSuccess('套餐已保存，已购买权益保持购买时的规则'); emit('saved'); emit('close') }
 catch (e) { error.value = e instanceof Error ? e.message : '保存失败，请重试' }
 finally { submitting.value = false }
}
</script>
<template>
 <BaseDialog :show="show" :title="plan ? '编辑通用订阅套餐' : '新建通用订阅套餐'" width="wide" :show-close-button="!submitting" @close="emit('close')">
  <form id="universal-plan-form" class="space-y-5" @submit.prevent="save">
   <p class="rounded-xl bg-primary-50 px-4 py-3 text-sm leading-6 text-primary-800 dark:bg-primary-950/30 dark:text-primary-200">一份额度，跨 Key、跨分组使用。到期、额度用尽或达到周期限额后停止调用，不接续普通余额。</p>
   <fieldset :disabled="submitting" class="grid gap-4 sm:grid-cols-2">
    <div class="sm:col-span-2"><label for="plan-name" class="input-label">套餐名称</label><input id="plan-name" v-model="form.name" class="input" maxlength="100" required placeholder="例如：轻享月卡" /></div>
    <div><label for="plan-price" class="input-label">售价</label><input id="plan-price" v-model.number="form.price" type="number" min="0.01" step="0.01" class="input" required /></div>
    <div><label for="plan-original" class="input-label">划线原价（可选）</label><input id="plan-original" v-model.number="form.original_price" type="number" min="0" step="0.01" class="input" /></div>
    <div><label for="plan-validity" class="input-label">有效期（天）</label><input id="plan-validity" v-model.number="form.validity_days" type="number" min="1" max="36500" step="1" class="input" required /></div>
    <div><label for="plan-total" class="input-label">套餐总额度（USD）</label><input id="plan-total" v-model.number="form.total_credit_usd" type="number" min="0.00000001" step="0.01" class="input" required /></div>
    <div v-for="limit in limits" :key="limit.key"><label :for="limit.key" class="input-label">{{ limit.label }}（USD，0 为不限）</label><input :id="limit.key" v-model.number="form[limit.key]" type="number" min="0" step="0.01" class="input" /></div>
    <div><label for="plan-currency" class="input-label">售价币种标签（可选）</label><input id="plan-currency" v-model="form.currency" maxlength="3" class="input uppercase" placeholder="USD" /></div>
    <div class="sm:col-span-2"><label for="plan-product" class="input-label">支付商品名称（可选）</label><input id="plan-product" v-model="form.product_name" class="input" maxlength="100" placeholder="留空时使用站点名称与套餐名称" /></div>
    <div class="sm:col-span-2"><label for="plan-description" class="input-label">说明</label><textarea id="plan-description" v-model="form.description" rows="2" class="input" maxlength="4000" /></div>
    <div class="sm:col-span-2"><label for="plan-features" class="input-label">权益说明（每行一条）</label><textarea id="plan-features" v-model="form.features" rows="3" class="input" /></div>
    <div><label for="plan-sort" class="input-label">排序</label><input id="plan-sort" v-model.number="form.sort_order" type="number" class="input" /></div>
    <label class="flex items-center gap-2 text-sm"><input v-model="form.for_sale" type="checkbox" class="checkbox" />上架销售</label>
   </fieldset>
   <div v-if="subscriptionCnyPreview" class="rounded-xl bg-gray-50 p-3 text-xs leading-6 text-gray-600 dark:bg-dark-700 dark:text-gray-300"><p>{{ t('payment.admin.subscriptionCnyPayPreview', { amount: subscriptionCnyPreview.amount }) }}</p><p v-if="subscriptionCnyPreview.feeRate > 0">{{ t('payment.admin.subscriptionCnyPayPreviewWithFee', { feeRate: subscriptionCnyPreview.feeRate, total: subscriptionCnyPreview.total }) }}</p></div>
   <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">每日额度按站点时区零点重置；周按 7 天、月按 30 天滚动。周期刷新不补回套餐总额度。不同分组按各自实际倍率扣费，专属分组仍需授权。</p>
   <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
  </form>
  <template #footer><button class="btn btn-secondary" :disabled="submitting" @click="emit('close')">取消</button><button type="submit" form="universal-plan-form" class="btn btn-primary" :disabled="submitting">{{ submitting ? '保存中…' : '保存套餐' }}</button></template>
 </BaseDialog>
</template>
