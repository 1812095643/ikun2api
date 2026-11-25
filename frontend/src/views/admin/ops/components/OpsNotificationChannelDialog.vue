<script setup lang="ts">
import { computed, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import { opsMonitorAPI, type ChannelKind, type NotificationChannel, type NotificationChannelInput, type NotificationTarget } from '@/api/admin/opsMonitor'

const props = defineProps<{ show: boolean; channel?: NotificationChannel | null; targets: NotificationTarget[] }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const form = ref<NotificationChannelInput | null>(null), saving = shallowRef(false), error = shallowRef(''), testing = shallowRef(''), testResult = shallowRef(''), testTargetID = shallowRef('')
const kinds: ChannelKind[] = ['email', 'feishu_webhook', 'feishu_app', 'lark_webhook', 'lark_app', 'telegram', 'wecom', 'dingtalk', 'qq', 'webhook']
const kindOptions = computed(() => kinds.map(value => ({ value, label: t(`admin.ops.enhanced.channels.${value}`) })))
const webhook = computed(() => !!form.value && ['feishu_webhook', 'lark_webhook', 'wecom', 'webhook'].includes(form.value.kind))
const app = computed(() => !!form.value && ['feishu_app', 'lark_app', 'dingtalk', 'qq'].includes(form.value.kind))
const isNew = computed(() => !form.value?.revision)
const secretFields = computed(() => !form.value ? [] : webhook.value ? ['webhook', ...(['wecom'].includes(form.value.kind) ? [] : ['signing_secret'])] : form.value.kind === 'telegram' ? ['bot_token'] : app.value ? ['app_secret'] : [])
const typeOptions = computed(() => form.value?.kind.endsWith('_app') ? ['chat_id', 'open_id', 'user_id'].map(value => ({ value, label: t(`admin.ops.enhanced.targetTypes.${value}`) })) : ['group', 'user'].map(value => ({ value, label: t(`admin.ops.enhanced.targetTypes.${value}`) })))
function addTarget() {
  if (!form.value) return
  form.value.targets.push({ id: crypto.randomUUID(), channel_id: form.value.id, name: '', address: webhook.value ? 'group' : '', enabled: true, settings: { type: form.value.kind.endsWith('_app') ? 'chat_id' : 'group' } })
}
function reset() {
  const c = props.channel
  form.value = c ? { ...JSON.parse(JSON.stringify(c)), settings: { ...c.settings }, secrets: {}, clear_secrets: [], targets: JSON.parse(JSON.stringify(props.targets.filter(target => target.channel_id === c.id))) } : { id: crypto.randomUUID(), kind: 'feishu_webhook', name: '', enabled: false, settings: {}, secret_configured: [], revision: 0, secrets: {}, clear_secrets: [], targets: [] }
  if (form.value && !form.value.targets.length) addTarget()
  testTargetID.value = form.value?.targets[0]?.id || ''
  error.value = ''; testResult.value = ''
}
watch(() => props.show, show => { if (show) reset() }, { immediate: true })
watch(() => form.value?.kind, (kind, previous) => { if (!kind || !previous || !isNew.value) return; if (form.value) { form.value.settings = {}; form.value.secrets = {}; form.value.targets = []; addTarget() } })
function clearSecret(key: string, checked: boolean) { if (!form.value) return; form.value.clear_secrets = checked ? [...form.value.clear_secrets, key] : form.value.clear_secrets.filter(value => value !== key) }
async function save(andTest = false) {
  if (!form.value || saving.value) return
  saving.value = true; error.value = ''
  try {
    const wanted = form.value.targets.find(item => item.id === testTargetID.value) || form.value.targets[0]
    const result = await opsMonitorAPI.saveChannel(form.value)
    emit('saved')
    if (!andTest) { emit('close'); return }
    const savedChannel = result.channels.find(item => item.id === form.value?.id)
    if (savedChannel) { form.value.revision = savedChannel.revision; form.value.secret_configured = savedChannel.secret_configured }
    form.value.secrets = {}; form.value.clear_secrets = []
    const target = result.targets.find(item => item.channel_id === form.value?.id && item.address === wanted?.address)
    if (target) await test(target)
  }
  catch (e: any) { error.value = e?.message || t('admin.ops.enhanced.saveError') }
  finally { saving.value = false }
}
async function test(target: NotificationTarget) {
  if (!form.value) return
  testing.value = target.id; testResult.value = ''; error.value = ''
  try { const data = await opsMonitorAPI.testChannel(form.value.id, target.id); testResult.value = t(`admin.ops.enhanced.deliveryState.${data.state}`); if (data.error_class) error.value = t(`admin.ops.enhanced.deliveryError.${data.error_class}`) }
  catch (e: any) { error.value = e?.message || t('admin.ops.enhanced.saveError') }
  finally { testing.value = '' }
}
</script>

<template>
  <BaseDialog :show="show" :title="t('admin.ops.enhanced.channelConfig')" width="wide" :z-index="70" @close="emit('close')">
    <form v-if="form" class="space-y-4" @submit.prevent="save()">
      <p v-if="error" class="text-sm text-red-600" role="alert">{{ error }}</p>
      <p v-if="testResult" class="text-sm text-emerald-600" role="status">{{ testResult }}</p>
      <div class="grid gap-3 sm:grid-cols-2"><div><label class="input-label">{{ t('admin.ops.enhanced.name') }}</label><input v-model="form.name" class="input" maxlength="100" required /></div><div><label class="input-label">{{ t('admin.ops.enhanced.platform') }}</label><Select v-model="form.kind" :options="kindOptions" :disabled="!isNew" /></div></div>
      <p v-if="form.kind === 'email'" class="text-xs text-gray-500">{{ t('admin.ops.enhanced.systemSMTP') }}</p>
      <div v-if="app" class="grid gap-3 sm:grid-cols-2"><div><label class="input-label">App ID / App Key</label><input v-model="form.settings.app_id" class="input" autocomplete="off" /></div><div v-if="form.kind === 'dingtalk'"><label class="input-label">Robot Code</label><input v-model="form.settings.robot_code" class="input" /></div></div>
      <div v-for="key in secretFields" :key="key"><div class="mb-1 flex items-center justify-between"><label class="input-label !mb-0">{{ t(`admin.ops.enhanced.secretFields.${key}`) }}</label><label v-if="form.secret_configured.includes(key)" class="flex items-center gap-1.5 text-xs text-gray-400"><input type="checkbox" :checked="form.clear_secrets.includes(key)" @change="clearSecret(key, ($event.target as HTMLInputElement).checked)" />{{ t('admin.ops.enhanced.clearSecret') }}</label></div><input v-model="form.secrets[key]" type="password" autocomplete="new-password" class="input" :disabled="form.clear_secrets.includes(key)" :placeholder="form.secret_configured.includes(key) ? t('admin.ops.enhanced.keepSecret') : ''" /></div>
      <div class="flex items-center justify-between border-t border-gray-100 pt-3 dark:border-dark-700"><h4 class="text-sm font-medium">{{ t('admin.ops.enhanced.targets') }}</h4><button v-if="!webhook" class="btn btn-ghost btn-sm" type="button" :disabled="form.targets.length >= 50" @click="addTarget">{{ t('common.add') }}</button></div>
      <div v-for="(target, index) in form.targets" :key="target.id" class="space-y-2 rounded-xl bg-gray-50 p-3 dark:bg-dark-800">
        <div class="flex items-center gap-2"><input v-model="target.name" class="input min-w-0 flex-1" :placeholder="t('admin.ops.enhanced.targetName')" required /><Toggle v-model="target.enabled" /><button v-if="!webhook" class="btn btn-ghost btn-sm" type="button" @click="form.targets.splice(index, 1)">{{ t('common.delete') }}</button></div>
        <div v-if="!webhook" class="flex gap-2"><Select v-if="app && form.kind !== 'email'" v-model="target.settings.type" :options="typeOptions" class="w-32 shrink-0" /><input v-model="target.address" class="input min-w-0 flex-1" :placeholder="form.kind === 'email' ? t('admin.ops.enhanced.emailAddress') : t('admin.ops.enhanced.targetAddress')" required /></div>
        <input v-if="form.kind === 'telegram'" v-model="target.settings.thread_id" class="input" :placeholder="t('admin.ops.enhanced.thread')" inputmode="numeric" />
        <input v-if="form.kind === 'wecom'" v-model="target.settings.mentions" class="input" :placeholder="t('admin.ops.enhanced.mentions')" />
        <div v-if="!isNew && targets.some(item => item.id === target.id)" class="flex justify-end"><button class="btn btn-ghost btn-sm" type="button" :disabled="!!testing" @click="test(target)">{{ t(testing === target.id ? 'admin.ops.enhanced.testing' : 'admin.ops.enhanced.testSaved') }}</button></div>
      </div>
      <p v-if="form.kind === 'qq'" class="text-xs text-amber-600">{{ t('admin.ops.enhanced.qqHint') }}</p>
      <div class="flex items-center justify-between"><label class="text-sm">{{ t('admin.ops.enhanced.enableChannel') }}</label><Toggle v-model="form.enabled" /></div>
      <button type="submit" class="hidden" />
    </form>
    <template #footer><Select v-if="form && form.targets.length > 1" v-model="testTargetID" :options="form.targets.map(item => ({ value: item.id, label: item.name || item.address }))" class="mr-auto w-40" /><button class="btn btn-secondary" @click="emit('close')">{{ t('common.cancel') }}</button><button class="btn btn-secondary" :disabled="saving || !!testing || !form?.name.trim() || !form?.targets.length" @click="save(true)">{{ t('admin.ops.enhanced.saveAndTest') }}</button><button class="btn btn-primary" :disabled="saving || !form?.name.trim()" @click="save()">{{ t(saving ? 'common.saving' : 'common.save') }}</button></template>
  </BaseDialog>
</template>
