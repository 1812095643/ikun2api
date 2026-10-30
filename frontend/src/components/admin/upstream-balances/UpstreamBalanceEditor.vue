<script setup lang="ts">
import { computed, reactive, shallowRef, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import type { UpstreamBalance, UpstreamBalanceDraft } from '@/api/upstreamBalances'

const props = defineProps<{ item: UpstreamBalance | null; saving: boolean }>()
const emit = defineEmits<{ close: []; save: [id: string, draft: UpstreamBalanceDraft] }>()
const form = reactive<UpstreamBalanceDraft>({ name: '', config: { kind: 'auto', auth_mode: 'auto', new_api_user_id: 0, preferred_account_id: 0, monitor_enabled: false, interval_minutes: 30, threshold: 10, recipients: [] } })
const token = shallowRef('')
const recipients = shallowRef('')
const showWallet = shallowRef(false)
const validation = shallowRef('')
const keyOptions = computed(() => [{ value: 0, label: '自动（仅使用一致的读数）' }, ...(props.item?.accounts.map(a => ({ value: a.id, label: `#${a.id} ${a.name}` })) || [])])
const kinds = [{ value: 'auto', label: '自动识别' }, { value: 'sub2api', label: 'Sub2API' }, { value: 'newapi', label: 'New API' }]
const modes = [{ value: 'auto', label: '自动兼容' }, { value: 'modern', label: '新版（Bearer）' }, { value: 'legacy', label: '旧版（原始令牌）' }]
watch(() => props.item, item => {
  token.value = ''; validation.value = ''
  if (!item) return
  form.name = item.name
  form.config = { ...item.config, recipients: [...item.config.recipients] }
  form.clear_wallet_token = false
  recipients.value = item.config.recipients.join('\n')
  showWallet.value = item.token_configured
}, { immediate: true })
function submit() {
  if (!props.item || props.saving) return
  if (!form.name.trim()) { validation.value = '请填写供应商名称。'; return }
  if (form.config.auth_mode === 'legacy' && !form.config.new_api_user_id) { validation.value = '旧版 New API 需要填写用户 ID。'; return }
  const list = recipients.value.split(/[\n,，;]/).map(v => v.trim()).filter(Boolean)
  if (list.length > 10 || list.some(v => !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v))) { validation.value = '请填写有效的提醒邮箱，最多 10 个。'; return }
  const draft: UpstreamBalanceDraft = { name: form.name.trim(), config: { ...form.config, recipients: list }, clear_wallet_token: form.clear_wallet_token }
  if (token.value.trim()) draft.wallet_token = token.value.trim()
  emit('save', props.item.id, draft)
}
</script>

<template>
  <BaseDialog :show="!!item" title="上游设置" width="normal" :close-on-escape="!saving" :show-close-button="!saving" @close="emit('close')">
    <form id="upstream-balance-form" class="space-y-5" @submit.prevent="submit">
      <div><label for="balance-provider-name" class="input-label">供应商名称</label><input id="balance-provider-name" v-model="form.name" class="input" maxlength="100" required /><p class="mt-1 break-all text-xs text-gray-500 dark:text-dark-400">{{ item?.url }}</p></div>
      <div><label class="input-label" for="balance-provider-kind">站点类型</label><Select id="balance-provider-kind" v-model="form.config.kind" :options="kinds" /></div>
      <div><label class="input-label" for="balance-provider-source">监控 Key</label><Select id="balance-provider-source" v-model="form.config.preferred_account_id" :options="keyOptions" /><p class="mt-1 text-xs text-gray-500 dark:text-dark-400">同站多个 Key 会分别校验，余额不会相加。</p></div>
      <div v-if="form.config.kind !== 'sub2api'" class="rounded-lg border border-gray-200 dark:border-dark-600">
        <button type="button" class="flex w-full items-center justify-between px-4 py-3 text-sm font-medium text-gray-700 dark:text-gray-200" :aria-expanded="showWallet" @click="showWallet = !showWallet"><span>New API 钱包凭据 <span class="ml-1 text-xs font-normal text-gray-500">{{ item?.token_configured ? '已配置' : '选填' }}</span></span><span>{{ showWallet ? '收起' : '展开' }}</span></button>
        <div v-if="showWallet" class="space-y-4 border-t border-gray-200 p-4 dark:border-dark-600">
          <p class="text-xs text-gray-500 dark:text-dark-400">优先复用账号 Key。上游未开放钱包查询时，可填写个人访问令牌。</p>
          <div><label class="input-label" for="balance-wallet-token">钱包访问令牌</label><input id="balance-wallet-token" v-model="token" class="input" type="password" autocomplete="new-password" :placeholder="item?.token_configured ? '留空保留现有令牌' : '填写上游个人访问令牌'" maxlength="8192" /></div>
          <label v-if="item?.token_configured" class="flex items-center gap-2 text-sm text-gray-600 dark:text-dark-300"><input v-model="form.clear_wallet_token" type="checkbox" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />清除已保存的钱包令牌</label>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2"><div><label class="input-label" for="balance-wallet-auth">鉴权方式</label><Select id="balance-wallet-auth" v-model="form.config.auth_mode" :options="modes" /></div><div><label class="input-label" for="balance-wallet-user">上游用户 ID</label><input id="balance-wallet-user" v-model.number="form.config.new_api_user_id" class="input" type="number" min="0" step="1" /></div></div>
        </div>
      </div>
      <div class="border-t border-gray-200 pt-5 dark:border-dark-600"><div class="flex items-center justify-between"><label for="balance-monitor-enabled" class="text-sm font-medium text-gray-900 dark:text-gray-100">余额监控</label><Toggle id="balance-monitor-enabled" v-model="form.config.monitor_enabled" /></div></div>
      <template v-if="form.config.monitor_enabled">
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2"><div><label for="balance-monitor-interval" class="input-label">检查间隔（分钟）</label><input id="balance-monitor-interval" v-model.number="form.config.interval_minutes" class="input" type="number" min="5" max="1440" step="1" required /></div><div><label for="balance-monitor-threshold" class="input-label">低余额阈值</label><input id="balance-monitor-threshold" v-model.number="form.config.threshold" class="input" type="number" min="0" max="1000000000000" step="any" required /><p class="mt-1 text-xs text-gray-500 dark:text-dark-400">单位与上游显示一致。</p></div></div>
        <div><label for="balance-monitor-recipients" class="input-label">提醒邮箱</label><textarea id="balance-monitor-recipients" v-model="recipients" class="input" rows="2" placeholder="一行一个；留空仅在本页提示" /><p class="mt-1 text-xs text-gray-500 dark:text-dark-400">复用系统发信设置；低余额持续期间不重复发送。</p></div>
      </template>
      <p v-if="validation" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ validation }}</p>
    </form>
    <template #footer><button type="button" class="btn btn-secondary" :disabled="saving" @click="emit('close')">取消</button><button type="submit" form="upstream-balance-form" class="btn btn-primary" :disabled="saving">{{ saving ? '保存中…' : '保存' }}</button></template>
  </BaseDialog>
</template>
