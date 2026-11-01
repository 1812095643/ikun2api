<script setup lang="ts">
import { nextTick, reactive, ref } from 'vue'
import { useAppStore } from '@/stores/app'
import type { ReloginOptions, ReloginSettings } from '@/api/relogin'
const props = defineProps<{ settings: ReloginSettings; options: ReloginOptions; busy: boolean; error?: string }>()
const emit = defineEmits<{ save: [settings: ReloginSettings] }>()
const appStore = useAppStore()
const draft = reactive<ReloginSettings>(JSON.parse(JSON.stringify(props.settings)))
const recipients = ref(props.settings.recipients.join('\n'))
const activeTab = ref('auto')
const tabs = [
  { value: 'auto', label: '自动重登' },
  { value: 'account', label: '账号默认值' },
  { value: 'quota', label: '额度通知' }
]
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
  emit('save', {
    ...draft,
    template: { ...draft.template },
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
    <p v-if="error" class="ws-error mb-4" role="alert">{{ error }}</p>
    <fieldset :disabled="busy" class="relogin-settings-fields">
    <section id="relogin-settings-panel-auto" v-show="activeTab === 'auto'" data-settings-tab="auto"
      class="relogin-settings-panel" role="tabpanel" aria-labelledby="relogin-settings-tab-auto">
      <label class="setting-check"
        ><input
          v-model="draft.auto_enabled"
          type="checkbox"
        />检测到凭据失效时，使用已保存的资料重登</label
      >
      <p class="ws-muted">
        仅处理明确的授权失效错误；同一账号间隔至少 6 小时，每次最多 20
        个。遇到额外验证时等待人工处理。
      </p>
    </section>
    <section id="relogin-settings-panel-account" v-show="activeTab === 'account'" data-settings-tab="account"
      class="relogin-settings-panel" role="tabpanel" aria-labelledby="relogin-settings-tab-account">
      <p class="ws-muted mb-4">仅用于验证成功后新建的账号。已有账号继续使用自己的分组与代理。</p>
      <div class="ws-form-grid">
        <label class="ws-field"
          >默认代理<select
            v-model="draft.template.proxy_id"
            class="ws-input"
          >
            <option :value="null">使用默认连接</option>
            <option
              v-for="proxy in options.proxies"
              :key="proxy.id"
              :value="proxy.id"
            >
              {{ proxy.name }}
            </option>
          </select></label
        ><label class="ws-field"
          >并发数<input
            v-model.number="draft.template.concurrency"
            class="ws-input"
            type="number"
            min="1"
            max="10000" /></label
        ><label class="ws-field"
          >优先级<input
            v-model.number="draft.template.priority"
            class="ws-input"
            type="number"
            min="0" /></label
        ><label class="ws-field"
          >账号倍率<input
            v-model.number="draft.template.rate_multiplier"
            class="ws-input"
            type="number"
            min="0"
            step="0.01"
        /></label>
      </div>
      <div class="ws-field mt-5">
        <span>分组</span>
        <div class="settings-groups">
          <label
            v-for="group in options.groups"
            :key="group.id"
            class="setting-check"
            ><input
              v-model="draft.template.group_ids"
              type="checkbox"
              :value="group.id"
            />{{ group.name }}</label
          >
          <p
            v-if="!options.groups.length"
            class="ws-muted"
          >
            暂无可选的 OpenAI 分组，请先到分组管理中创建。
          </p>
        </div>
      </div>
    </section>
    <section id="relogin-settings-panel-quota" v-show="activeTab === 'quota'" data-settings-tab="quota"
      class="relogin-settings-panel" role="tabpanel" aria-labelledby="relogin-settings-tab-quota">
      <label class="setting-check"
        ><input
          v-model="draft.quota_enabled"
          type="checkbox"
        />定期检查额度并通过本站邮件服务提醒</label
      >
      <div class="ws-form-grid mt-4">
        <label class="ws-field"
          >检查间隔（秒）<input
            v-model.number="draft.quota_interval"
            class="ws-input"
            type="number"
            min="60"
            max="86400" /></label
        ><label class="ws-field"
          >剩余额度合计低于（%）<input
            v-model.number="draft.quota_threshold"
            class="ws-input"
            type="number"
            min="0"
            step="1"
        /></label>
      </div>
      <label class="ws-field mt-4"
        >接收邮箱<textarea
          v-model="recipients"
          class="ws-input"
          rows="3"
          placeholder="每行一个邮箱"
        /><small>沿用 {{ appStore.siteName }} 中配置的邮件服务。只在额度跨过阈值时发送提醒。</small></label
      >
    </section>
    </fieldset>
  </form>
</template>
<style scoped>
.relogin-settings-fields {
  min-width: 0;
}
.relogin-settings-panel {
  min-height: 270px;
}
.setting-check {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  font-size: 13px;
  line-height: 1.7;
  margin-bottom: 9px;
}
.setting-check input {
  margin-top: 5px;
  accent-color: var(--ws-primary, #2563eb);
}
.settings-groups {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  padding: 13px;
  background: var(--ws-soft, #f5f7f8);
  border-radius: 9px;
}
@media (max-width: 600px) {
  .settings-groups {
    grid-template-columns: 1fr;
  }
}
</style>
