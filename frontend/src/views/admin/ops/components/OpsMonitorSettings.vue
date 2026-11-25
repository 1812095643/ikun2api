<script setup lang="ts">
import { computed, onMounted, ref, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import DataTable from '@/components/common/DataTable.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import { opsAPI, type AlertRule } from '@/api/admin/ops'
import { opsMonitorAPI, type MonitorConfig, type MonitorDelivery, type MonitorProfile, type MonitorRule, type NotificationChannel, type NotificationTarget, type NotificationPolicy } from '@/api/admin/opsMonitor'
import OpsNotificationChannelDialog from './OpsNotificationChannelDialog.vue'
import OpsNotificationPolicyDialog from './OpsNotificationPolicyDialog.vue'
import OpsDeliveryTable from './OpsDeliveryTable.vue'

const emit = defineEmits<{ saved: [] }>()
const { t } = useI18n()
const tab = shallowRef('rules'), loading = shallowRef(false), saving = shallowRef(false), error = shallowRef(''), success = shallowRef(false)
const config = ref<MonitorConfig | null>(null), rules = ref<MonitorRule[]>([]), legacyRules = ref<AlertRule[]>([])
const channels = ref<NotificationChannel[]>([]), targets = ref<NotificationTarget[]>([]), deliveries = ref<MonitorDelivery[]>([])
const channelDialog = shallowRef(false), selectedChannel = shallowRef<NotificationChannel | null>(null), policyDialog = shallowRef(false), selectedPolicy = shallowRef<NotificationPolicy | null>(null)
const profileModel = shallowRef(''), newLegacyRule = shallowRef(''), deliveryChannel = shallowRef(''), deliveryState = shallowRef('')
const channelColumns = computed(() => ['name', 'kind', 'targets', 'enabled', 'last_delivery', 'actions'].map(key => ({ key, label: t(`admin.ops.enhanced.channelColumns.${key}`) })))
const ownerOptions = computed(() => ['legacy', 'monitor'].map(value => ({ value, label: t(`admin.ops.enhanced.owner.${value}`) })))
const legacyOptions = computed(() => [{ value: '', label: t('admin.ops.enhanced.selectLegacy') }, ...legacyRules.value.map(value => ({ value: String(value.id), label: value.name }))])
const channelOptions = computed(() => [{ value: '', label: t('common.all') }, ...channels.value.map(value => ({ value: value.id, label: value.name }))])
const deliveryStateOptions = computed(() => [{ value: '', label: t('common.all') }, ...['pending', 'accepted', 'dead', 'uncertain', 'suppressed', 'shadow'].map(value => ({ value, label: t(`admin.ops.enhanced.deliveryState.${value}`) }))])
const profileFields = ['queue_warn_seconds', 'queue_fail_seconds', 'first_warn_seconds', 'first_fail_seconds', 'stall_warn_seconds', 'stall_fail_seconds', 'task_sla_seconds'] as const
async function load() {
  loading.value = true; error.value = ''
  try {
    const [monitor, list, old] = await Promise.all([opsMonitorAPI.config(), opsMonitorAPI.channels(), opsAPI.listAlertRules()])
    config.value = monitor.config; config.value.policies ??= []; config.value.legacy_handoffs ??= []; config.value.owner_overrides ??= []
    rules.value = monitor.rules; channels.value = list.channels; targets.value = list.targets; legacyRules.value = old
  } catch (e: any) { error.value = e?.message || t('admin.ops.enhanced.loadError') }
  finally { loading.value = false }
}
async function save() {
  if (!config.value) return
  saving.value = true; error.value = ''; success.value = false
  try { const result = await opsMonitorAPI.saveConfig(config.value); config.value = result.config; config.value.policies ??= []; config.value.legacy_handoffs ??= []; success.value = true; emit('saved') }
  catch (e: any) { error.value = e?.message || t('admin.ops.enhanced.saveError') }
  finally { saving.value = false }
}
async function refreshChannels() { try { const list = await opsMonitorAPI.channels(); channels.value = list.channels; targets.value = list.targets; emit('saved') } catch (e: any) { error.value = e?.message || t('admin.ops.enhanced.loadError') } }
async function loadDeliveries() { loading.value = true; try { deliveries.value = await opsMonitorAPI.deliveries({ channel: deliveryChannel.value || undefined, state: deliveryState.value || undefined }) } catch (e: any) { error.value = e?.message || t('admin.ops.enhanced.loadError') } finally { loading.value = false } }
function changeTab(value: string) { tab.value = value; error.value = ''; success.value = false; if (value === 'deliveries') void loadDeliveries() }
function editChannel(channel: NotificationChannel | null) { selectedChannel.value = channel; channelDialog.value = true }
function editPolicy(policy: NotificationPolicy | null) { selectedPolicy.value = policy; policyDialog.value = true }
function applyPolicy(policy: NotificationPolicy) { if (!config.value) return; const index = config.value.policies.findIndex(item => item.id === policy.id); if (index >= 0) config.value.policies.splice(index, 1, policy); else config.value.policies.push(policy) }
function policyTargets(policy: NotificationPolicy) { return policy.target_ids.map(id => targets.value.find(target => target.id === id)?.name || t('admin.ops.enhanced.removedTarget')).join(' · ') }
function addProfile() { const model = profileModel.value.trim(); if (!config.value || !model || model in config.value.profiles) return; config.value.profiles[model] = { ...config.value.profiles.default }; profileModel.value = '' }
function addHandoff() { if (!config.value || !newLegacyRule.value) return; const id = Number(newLegacyRule.value); if (config.value.legacy_handoffs.some(item => item.legacy_rule_id === id)) return; config.value.legacy_handoffs.push({ legacy_rule_id: id, monitor_rules: [] }); newLegacyRule.value = '' }
function toggleRule(handoff: { monitor_rules: string[] }, id: string, checked: boolean) { handoff.monitor_rules = checked ? [...handoff.monitor_rules, id] : handoff.monitor_rules.filter(value => value !== id) }
function optionalNumber(value: string) { const n = Number(value); return value.trim() && Number.isFinite(n) ? n : undefined }
const profileEntries = computed(() => config.value ? Object.entries(config.value.profiles) as [string, MonitorProfile][] : [])
onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <nav class="flex gap-4 overflow-x-auto border-b border-gray-100 dark:border-dark-700"><button v-for="value in ['rules', 'channels', 'policies', 'deliveries']" :key="value" class="whitespace-nowrap border-b-2 py-2 text-sm" :class="tab === value ? 'border-primary-500 text-primary-600' : 'border-transparent text-gray-400'" @click="changeTab(value)">{{ t(`admin.ops.enhanced.settingsTabs.${value}`) }}</button></nav>
    <p v-if="error" class="text-sm text-red-600" role="alert">{{ error }}</p><p v-if="success" class="text-sm text-emerald-600" role="status">{{ t('admin.ops.enhanced.saved') }}</p>
    <p v-if="loading && !config" class="py-10 text-center text-sm text-gray-400">{{ t('common.loading') }}</p>
    <template v-if="config">
      <template v-if="tab === 'rules'">
        <div class="grid items-end gap-4 sm:grid-cols-3"><label class="flex h-10 items-center justify-between gap-3 text-sm">{{ t('admin.ops.enhanced.recordOnly') }}<Toggle v-model="config.shadow" /></label><div><label class="input-label">{{ t('admin.ops.enhanced.siteName') }}</label><input v-model="config.site_name" class="input" maxlength="120" :placeholder="t('admin.ops.enhanced.followSite')" /></div><div><label class="input-label">{{ t('admin.ops.enhanced.retention') }}</label><input v-model.number="config.retention_days" class="input" type="number" min="7" max="365" /></div></div>
        <div class="grid gap-3 sm:grid-cols-3"><div><label class="input-label">{{ t('admin.ops.enhanced.balanceThreshold') }}</label><input :value="config.balance_low_usd ?? ''" class="input" type="number" min="0" step="0.01" @input="config.balance_low_usd = optionalNumber(($event.target as HTMLInputElement).value)" /></div><div><label class="input-label">{{ t('admin.ops.enhanced.paymentGrace') }}</label><input v-model.number="config.payment_grace_seconds" class="input" type="number" min="30" /></div><div><label class="input-label">{{ t('admin.ops.enhanced.billingGrace') }}</label><input v-model.number="config.billing_grace_seconds" class="input" type="number" min="30" /></div></div>
        <div><label class="input-label">{{ t('admin.ops.enhanced.detailURL') }}</label><input v-model="config.detail_base_url" class="input" type="url" placeholder="https://example.com" /></div>
        <div class="divide-y divide-gray-100 dark:divide-dark-700"><div v-for="rule in rules" :key="rule.id" class="grid grid-cols-[auto_minmax(0,1fr)_150px] items-center gap-3 py-2.5"><Toggle v-model="config.rules[rule.id]!.enabled" /><span class="text-sm"><span class="mr-2 font-mono text-xs text-gray-400">{{ rule.id }}</span>{{ t(`admin.ops.enhanced.rules.${rule.id}`) }}</span><Select v-model="config.rules[rule.id]!.notification_owner" :options="ownerOptions" /></div></div>
        <details class="rounded-xl bg-gray-50 p-3 dark:bg-dark-800"><summary class="cursor-pointer text-sm">{{ t('admin.ops.enhanced.profiles') }}</summary><div class="mt-3 space-y-3"><div class="flex gap-2"><input v-model="profileModel" class="input min-w-0 flex-1" :placeholder="t('admin.ops.enhanced.profileModel')" /><button class="btn btn-secondary" :disabled="!profileModel.trim()" @click="addProfile">{{ t('common.add') }}</button></div><div v-for="[model, profile] in profileEntries" :key="model" class="border-t border-gray-200 pt-3 dark:border-dark-700"><div class="mb-2 flex items-center justify-between text-sm"><span>{{ model === 'default' ? t('admin.ops.enhanced.defaultProfile') : model }}</span><button v-if="model !== 'default'" class="btn btn-ghost btn-sm" @click="delete config.profiles[model]">{{ t('common.delete') }}</button></div><div class="grid grid-cols-2 gap-2 sm:grid-cols-3"><div v-for="field in profileFields" :key="field"><label class="input-label !text-xs">{{ t(`admin.ops.enhanced.profileFields.${field}`) }}</label><input v-model.number="profile[field]" class="input" type="number" min="0" /></div></div></div></div></details>
        <details class="rounded-xl bg-gray-50 p-3 dark:bg-dark-800"><summary class="cursor-pointer text-sm">{{ t('admin.ops.enhanced.handoff') }}</summary><div class="mt-3 space-y-3"><p class="text-xs text-gray-500">{{ t('admin.ops.enhanced.handoffHint') }}</p><div class="flex gap-2"><Select v-model="newLegacyRule" :options="legacyOptions" class="min-w-0 flex-1" /><button class="btn btn-secondary" :disabled="!newLegacyRule" @click="addHandoff">{{ t('common.add') }}</button></div><div v-for="(handoff, index) in config.legacy_handoffs" :key="`${handoff.legacy_rule_id}-${index}`" class="border-t border-gray-200 pt-3 dark:border-dark-700"><div class="mb-2 flex items-center justify-between text-sm"><span>{{ legacyRules.find(rule => rule.id === handoff.legacy_rule_id)?.name || handoff.legacy_rule_id }}</span><button class="btn btn-ghost btn-sm" @click="config.legacy_handoffs.splice(index, 1)">{{ t('common.delete') }}</button></div><div class="grid max-h-40 gap-2 overflow-y-auto sm:grid-cols-2"><label v-for="rule in rules" :key="rule.id" class="flex items-center gap-2 text-xs"><input type="checkbox" :checked="handoff.monitor_rules.includes(rule.id)" @change="toggleRule(handoff, rule.id, ($event.target as HTMLInputElement).checked)" />{{ rule.id }} · {{ t(`admin.ops.enhanced.rules.${rule.id}`) }}</label></div></div></div></details>
      </template>
      <template v-else-if="tab === 'channels'">
        <div class="flex justify-end"><button class="btn btn-primary btn-sm" @click="editChannel(null)">{{ t('admin.ops.enhanced.addChannel') }}</button></div>
        <DataTable :data="channels" :columns="channelColumns" row-key="id"><template #cell-kind="{ row }">{{ t(`admin.ops.enhanced.channels.${row.kind}`) }}</template><template #cell-targets="{ row }">{{ targets.filter(target => target.channel_id === row.id).length }}</template><template #cell-enabled="{ row }"><span class="text-xs" :class="row.enabled ? 'text-emerald-600' : 'text-gray-400'">{{ t(row.enabled ? 'admin.ops.enhanced.enabled' : 'admin.ops.enhanced.disabled') }}</span></template><template #cell-last_delivery="{ row }"><span v-if="row.last_state" class="text-xs">{{ t(`admin.ops.enhanced.deliveryState.${row.last_state}`) }}</span><span v-else class="text-gray-400">—</span><p v-if="row.last_error" class="mt-0.5 text-xs text-amber-600">{{ t(`admin.ops.enhanced.deliveryError.${row.last_error}`) }}</p><p v-if="row.last_accepted" class="mt-0.5 whitespace-nowrap text-xs text-gray-400">{{ new Date(row.last_accepted).toLocaleString() }}</p></template><template #cell-actions="{ row }"><button class="btn btn-ghost btn-sm" @click="editChannel(row)">{{ t('common.edit') }}</button></template></DataTable>
      </template>
      <template v-else-if="tab === 'policies'">
        <div class="flex justify-end"><button class="btn btn-primary btn-sm" @click="editPolicy(null)">{{ t('admin.ops.enhanced.addPolicy') }}</button></div>
        <div v-if="!config.policies.length" class="py-10 text-center text-sm text-gray-400">{{ t('admin.ops.enhanced.noPolicies') }}</div>
        <div v-for="(policy, index) in config.policies" :key="policy.id" class="flex items-center gap-3 border-b border-gray-100 py-3 dark:border-dark-700"><Toggle v-model="policy.enabled" /><div class="min-w-0 flex-1"><div class="text-sm font-medium">{{ policy.name }}</div><p class="mt-1 truncate text-xs text-gray-400">{{ policyTargets(policy) }}</p></div><button class="btn btn-ghost btn-sm" @click="editPolicy(policy)">{{ t('common.edit') }}</button><button class="btn btn-ghost btn-sm" @click="config.policies.splice(index, 1)">{{ t('common.delete') }}</button></div>
      </template>
      <template v-else><div class="grid grid-cols-[1fr_1fr_auto] gap-2"><Select v-model="deliveryChannel" :options="channelOptions" /><Select v-model="deliveryState" :options="deliveryStateOptions" /><button class="btn btn-secondary" @click="loadDeliveries">{{ t('common.refresh') }}</button></div><OpsDeliveryTable :items="deliveries" :targets="targets" :channels="channels" :loading="loading" /></template>
      <div v-if="tab === 'rules' || tab === 'policies'" class="flex justify-end border-t border-gray-100 pt-3 dark:border-dark-700"><button class="btn btn-primary" :disabled="saving" @click="save">{{ t(saving ? 'common.saving' : 'common.save') }}</button></div>
    </template>
    <OpsNotificationChannelDialog :show="channelDialog" :channel="selectedChannel" :targets="targets" @close="channelDialog = false" @saved="refreshChannels" />
    <OpsNotificationPolicyDialog :show="policyDialog" :value="selectedPolicy" :targets="targets" :channels="channels" :rules="rules" @close="policyDialog = false" @save="applyPolicy" />
  </div>
</template>
