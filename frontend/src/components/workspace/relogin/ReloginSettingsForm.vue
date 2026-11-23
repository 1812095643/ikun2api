<script setup lang="ts">
import { computed, nextTick, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import GroupSelector from '@/components/common/GroupSelector.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import ReloginModelSettings from './ReloginModelSettings.vue'
import { buildModelMappingObject, isValidWildcardPattern, splitModelMappingObject } from '@/composables/useModelWhitelist'
import type { Group } from '@/types'
import type { ReloginOptions, ReloginSettings } from '@/api/relogin'
const props = defineProps<{ settings: ReloginSettings; options: ReloginOptions; busy: boolean; error?: string }>()
const emit = defineEmits<{ save: [settings: ReloginSettings] }>()
const appStore = useAppStore()
const { t } = useI18n()
const draft = reactive<ReloginSettings>(JSON.parse(JSON.stringify(props.settings)))
const parsedModels = splitModelMappingObject(draft.template.model_mapping)
const allowedModels = ref(parsedModels.allowedModels)
const modelMappings = ref(parsedModels.modelMappings)
const validationError = ref('')
const recipients = ref(props.settings.recipients.join('\n'))
const activeTab = ref('auto')
const tabs = [
  { value: 'auto', label: '自动重登' },
  { value: 'account', label: '账号默认值' },
  { value: 'quota', label: '额度通知' }
]
const groups = computed(() => props.options.groups.map(group => ({ ...group, platform: group.platform || 'openai' })) as (Group & { account_count?: number })[])
const fingerprintOptions = computed(() => [
  { value: 'off', label: t('admin.accounts.openai.codexFingerprintOff') },
  { value: 'device', label: t('admin.accounts.openai.codexFingerprintDevice') },
  { value: 'session', label: t('admin.accounts.openai.codexFingerprintSession') },
  { value: 'full', label: t('admin.accounts.openai.codexFingerprintFull') }
])
const websocketOptions = computed(() => [
  { value: 'off', label: t('admin.accounts.openai.wsModeOff') },
  { value: 'ctx_pool', label: t('admin.accounts.openai.wsModeCtxPool') },
  { value: 'passthrough', label: t('admin.accounts.openai.wsModePassthrough') },
  { value: 'http_bridge', label: t('admin.accounts.openai.wsModeHttpBridge') }
])
async function revealInvalid(event: Event) {
  const input = event.target as HTMLInputElement
  const tab = input.closest<HTMLElement>('[data-settings-tab]')?.dataset.settingsTab
  if (!tab) return
  activeTab.value = tab
  await nextTick()
  input.focus()
}
function save() {
  if (props.busy) return
  validationError.value = ''
  const sources = new Set<string>()
  for (const mapping of modelMappings.value) {
    const from = mapping.from.trim(), to = mapping.to.trim()
    if (!from && !to) continue
    if (!from || !to) validationError.value = '请填写完整的来源模型和上游模型，或删除该行。'
    else if (!isValidWildcardPattern(from) || to.includes('*')) validationError.value = '来源模型的 * 只能放在末尾，上游模型不能包含 *。'
    else if (sources.has(from)) validationError.value = `来源模型「${from}」重复，请保留一条映射。`
    else if (from !== to && allowedModels.value.includes(from)) validationError.value = `「${from}」已在模型白名单中，请先移除再设置映射。`
    if (validationError.value) {
      activeTab.value = 'account'
      return
    }
    sources.add(from)
  }
  // 两个标签共用账号编辑的合并规则，切换展示方式不会丢失另一类配置。
  const mapping = buildModelMappingObject('combined', allowedModels.value, modelMappings.value) || {}
  emit('save', {
    ...draft,
    template: { ...draft.template,
      group_ids: draft.template.group_ids.filter(id => props.options.groups.some(group => group.id === id)),
      model_mapping: mapping, extra: {
      ...draft.template.extra,
      openai_oauth_responses_websockets_v2_enabled: draft.template.extra.openai_oauth_responses_websockets_v2_mode !== 'off'
    } },
    recipients: recipients.value
      .split(/[\s,;，；]+/)
      .map((value) => value.trim())
      .filter(Boolean)
  })
}
</script>
<template>
  <form
    id="relogin-settings-form"
    class="relogin-settings"
    @submit.prevent="save"
    @invalid.capture="revealInvalid"
  >
    <div class="ws-tabs" role="tablist" aria-label="失效重登设置分类">
      <button v-for="item in tabs" :id="`relogin-settings-tab-${item.value}`" :key="item.value"
        type="button" class="ws-tab" role="tab" :aria-selected="activeTab === item.value"
        :aria-controls="`relogin-settings-panel-${item.value}`" :disabled="busy" @click="activeTab = item.value">
        {{ item.label }}
      </button>
    </div>
    <p v-if="error || validationError" class="ws-error mb-4" role="alert">{{ error || validationError }}</p>
    <fieldset :disabled="busy" :inert="busy || undefined" class="relogin-settings-fields">
    <section id="relogin-settings-panel-auto" v-show="activeTab === 'auto'" data-settings-tab="auto"
      class="relogin-settings-panel" role="tabpanel" aria-labelledby="relogin-settings-tab-auto">
      <div class="flex items-center justify-between gap-4">
        <div>
          <label class="input-label mb-0" for="relogin-auto-enabled">自动重登</label>
          <p class="input-hint">凭据失效时使用已保存资料重登，同一账号间隔至少 6 小时。</p>
        </div>
        <Toggle id="relogin-auto-enabled" v-model="draft.auto_enabled" aria-label="自动重登" :disabled="busy" />
      </div>
    </section>
    <section id="relogin-settings-panel-account" v-show="activeTab === 'account'" data-settings-tab="account"
      class="relogin-settings-panel" role="tabpanel" aria-labelledby="relogin-settings-tab-account">
      <p class="input-hint mb-4">仅用于验证成功后新建的账号。</p>
      <div class="space-y-5">
        <div>
          <label class="input-label">{{ t('admin.accounts.proxy') }}</label>
          <ProxySelector v-model="draft.template.proxy_id" :proxies="options.proxies" :disabled="busy" />
        </div>
        <GroupSelector v-model="draft.template.group_ids" :groups="groups" platform="openai" />
        <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
          <div>
            <label class="input-label" for="relogin-concurrency">{{ t('admin.accounts.concurrency') }}</label>
            <input
            id="relogin-concurrency"
            v-model.number="draft.template.concurrency"
            class="input"
            type="number"
            min="1"
            max="10000" />
          </div>
          <div>
            <label class="input-label" for="relogin-load-factor">{{ t('admin.accounts.loadFactor') }}</label>
            <input id="relogin-load-factor" v-model.number="draft.template.load_factor" class="input" type="number" min="1" max="10000" :placeholder="String(draft.template.concurrency || 1)" @input="draft.template.load_factor = draft.template.load_factor ? Math.max(1, draft.template.load_factor) : null" />
          </div>
          <div>
            <label class="input-label" for="relogin-priority">{{ t('admin.accounts.priority') }}</label>
            <input
            id="relogin-priority"
            v-model.number="draft.template.priority"
            class="input"
            type="number"
            min="0" />
          </div>
          <div>
            <label class="input-label" for="relogin-rate-multiplier">{{ t('admin.accounts.billingRateMultiplier') }}</label>
            <input
            id="relogin-rate-multiplier"
            v-model.number="draft.template.rate_multiplier"
            class="input"
            type="number"
            min="0"
            step="0.001" />
          </div>
        </div>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label class="input-label" for="relogin-fingerprint-mode">{{ t('admin.accounts.openai.codexFingerprintMode') }}</label>
            <Select id="relogin-fingerprint-mode" v-model="draft.template.extra.codex_fingerprint_mode" :options="fingerprintOptions" aria-label="收敛档位" :disabled="busy" />
          </div>
          <div>
            <label class="input-label" for="relogin-ws-mode">{{ t('admin.accounts.openai.wsMode') }}</label>
            <Select id="relogin-ws-mode" v-model="draft.template.extra.openai_oauth_responses_websockets_v2_mode" :options="websocketOptions" aria-label="WebSocket" :disabled="busy" />
          </div>
        </div>
        <div class="grid grid-cols-1 gap-x-6 gap-y-3 sm:grid-cols-2">
          <div class="flex items-center justify-between gap-3">
            <label class="text-sm text-gray-700 dark:text-gray-300" for="relogin-auto-pause">到期自动暂停</label>
            <Toggle id="relogin-auto-pause" v-model="draft.template.auto_pause_on_expired" aria-label="到期自动暂停" :disabled="busy" />
          </div>
          <div class="flex items-center justify-between gap-3">
            <label class="text-sm text-gray-700 dark:text-gray-300" for="relogin-long-context">长上下文计费</label>
            <Toggle id="relogin-long-context" v-model="draft.template.extra.openai_long_context_billing_enabled" aria-label="长上下文计费" :disabled="busy" />
          </div>
        </div>
        <ReloginModelSettings v-model:allowed-models="allowedModels" v-model:model-mappings="modelMappings" :disabled="busy" />
      </div>
    </section>
    <section id="relogin-settings-panel-quota" v-show="activeTab === 'quota'" data-settings-tab="quota"
      class="relogin-settings-panel" role="tabpanel" aria-labelledby="relogin-settings-tab-quota">
      <div class="mb-4 flex items-center justify-between gap-4">
        <label class="input-label mb-0" for="relogin-quota-enabled">额度通知</label>
        <Toggle id="relogin-quota-enabled" v-model="draft.quota_enabled" aria-label="额度通知" :disabled="busy" />
      </div>
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div>
          <label class="input-label" for="relogin-quota-interval">检查间隔（秒）</label>
          <input
            id="relogin-quota-interval"
            v-model.number="draft.quota_interval"
            class="input"
            type="number"
            min="60"
            max="86400" />
        </div>
        <div>
          <label class="input-label" for="relogin-quota-threshold">剩余额度合计低于（%）</label>
          <input
            id="relogin-quota-threshold"
            v-model.number="draft.quota_threshold"
            class="input"
            type="number"
            min="0"
            step="1" />
        </div>
      </div>
      <div class="mt-4">
        <label class="input-label" for="relogin-recipients">接收邮箱</label>
        <textarea
          id="relogin-recipients"
          v-model="recipients"
          class="input"
          rows="3"
          placeholder="每行一个邮箱"
        />
        <p class="input-hint">使用 {{ appStore.siteName }} 的邮件服务，在额度跨过阈值时提醒。</p>
      </div>
    </section>
    </fieldset>
  </form>
</template>
<style scoped>
.relogin-settings-fields {
  min-width: 0;
}
</style>
