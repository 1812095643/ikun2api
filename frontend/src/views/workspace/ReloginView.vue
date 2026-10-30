<script setup lang="ts">
import { ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { useStepUp } from '@/composables/useStepUp'
import WorkspaceShell from '@/components/workspace/WorkspaceShell.vue'
import WorkspaceDialog from '@/components/workspace/WorkspaceDialog.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ReloginAccounts from '@/components/workspace/relogin/ReloginAccounts.vue'
import ReloginHistory from '@/components/workspace/relogin/ReloginHistory.vue'
import ReloginImported from '@/components/workspace/relogin/ReloginImported.vue'
import ReloginSettingsForm from '@/components/workspace/relogin/ReloginSettingsForm.vue'
import ReloginAuthorizationForm from '@/components/workspace/relogin/ReloginAuthorizationForm.vue'
import { useRelogin } from '@/composables/workspace/useRelogin'
import {
  reloginAPI,
  type InvalidImported,
  type LoginRun,
  type LoginSecret,
  type ReloginAccount,
  type ReloginSettings
} from '@/api/relogin'
import { workspaceError, formatTime } from '@/api/workspace'
const {
  tab,
  accounts,
  imported,
  history,
  settings,
  options,
  loading,
  busy,
  error,
  notice,
  search,
  status,
  importedQuery,
  importedPage,
  activeRuns,
  visibleAccounts,
  refresh,
  act
} = useRelogin()
const dialog = ref(''),
  loginAccount = ref<ReloginAccount | null>(null),
  selected = ref<string[]>([]),
  refreshToken = ref(''),
  invalid = ref<InvalidImported | null>(null),
  challenge = ref<LoginRun | null>(null),
  verifyCode = ref(''),
  phoneNumber = ref('')
const tabs = [
  { value: 'accounts', label: '渠道账号' },
  { value: 'imported', label: '登录资料' },
  { value: 'history', label: '授权记录' }
]
const stepUp = useStepUp()
const credentialRun = ref<LoginRun | null>(null), exportFormat = ref('sub2api')
const importProgress = ref('')
const exportFormats = [
  ['sub2api', 'IKUN2api / Sub2API'], ['codex', 'Codex CLI'], ['cpa', 'CPA'],
  ['cockpit', 'Cockpit'], ['9router', '9Router'], ['axonhub', 'AxonHub'],
  ['codexmanager', 'Codex Manager'], ['session', 'Session'], ['portable', '通用 JSON'],
  ['raw', '原始结果'], ['refresh_token', 'Refresh Token']
]
function openExport(run: LoginRun) {
  credentialRun.value = run
  exportFormat.value = 'sub2api'
  dialog.value = 'export'
}
async function exportCredential(copy: boolean) {
  const run = credentialRun.value, format = exportFormat.value
  if (!run) return
  closeDialog()
  await act(async () => {
    const result = await stepUp.run(() => reloginAPI.exportRun(run.id, format))
    const text = typeof result.content === 'string' ? result.content : JSON.stringify(result.content, null, 2)
    if (copy) {
      await navigator.clipboard.writeText(text)
      return
    }
    const url = URL.createObjectURL(new Blob([text], { type: format === 'refresh_token' ? 'text/plain' : 'application/json' }))
    const link = document.createElement('a')
    link.href = url
    link.download = 'ikun-auth-' + run.id + (format === 'refresh_token' ? '.txt' : '.json')
    link.click()
    setTimeout(() => URL.revokeObjectURL(url), 10000)
  }, copy ? '凭据已复制，请妥善保存。' : '授权凭据已导出。')
}
function openLogin(account: ReloginAccount | null) {
  if (busy.value) return
  error.value = ''
  loginAccount.value = account
  dialog.value = 'login'
}
function closeDialog() {
  dialog.value = ''
  refreshToken.value = ''
  verifyCode.value = ''
  phoneNumber.value = ''
  challenge.value = null
}
async function start(payload: LoginSecret & { account_id?: number; apply: boolean }) {
  if (await act(() => reloginAPI.start(payload), '授权任务已开始，可在授权记录中查看进度。')) {
    closeDialog()
    tab.value = 'history'
  }
}
async function startSaved(account: ReloginAccount) {
  await act(
    () =>
      reloginAPI.start({
        email: account.email,
        auth_mode: 'password_2fa',
        account_id: account.id,
        use_saved: true,
        apply: true
      }),
    '已使用保存的资料启动重登。'
  )
}
async function importSecrets(items: LoginSecret[], authorize: boolean, apply: boolean) {
  if (busy.value) return
  const accountId = loginAccount.value?.id
  let queued = 0
  const failures: string[] = []
  if (await act(async () => {
    await reloginAPI.import(items)
    if (!authorize) return
    // 保存成功后复用原有授权接口；只提交邮箱，从后端读取加密资料。
    for (const [index, item] of items.entries()) {
      importProgress.value = `正在提交第 ${index + 1} / ${items.length} 个授权任务…`
      try {
        await reloginAPI.start({ email: item.email, auth_mode: item.auth_mode,
          account_id: accountId, use_saved: true, apply })
        queued++
      } catch (e) {
        failures.push(`${item.email}：${workspaceError(e)}`)
      }
    }
  }, '登录资料已加密保存。')) {
    closeDialog()
    tab.value = authorize ? 'history' : 'imported'
    if (authorize) {
      notice.value = `已保存 ${items.length} 条资料，已提交 ${queued} 个授权任务。`
      if (failures.length) error.value = `有 ${failures.length} 个任务未确认提交，请先核对授权记录。${failures.slice(0, 3).join('；')}`
    }
  }
  importProgress.value = ''
}
async function saveSettings(next: ReloginSettings) {
  if (await act(() => reloginAPI.saveSettings(next), '设置已保存。')) closeDialog()
}
async function importRT() {
  if (await act(() => reloginAPI.refreshToken(refreshToken.value), '凭据已验证并写回本站账号。'))
    closeDialog()
}
async function exportSelected() {
  await act(async () => {
    const items = await stepUp.run(() => reloginAPI.export(selected.value))
    const url = URL.createObjectURL(
      new Blob([JSON.stringify(items, null, 2)], { type: 'application/json' })
    )
    const link = document.createElement('a')
    link.href = url
    link.download = 'ikun-login-records.json'
    link.click()
    setTimeout(() => URL.revokeObjectURL(url), 10000)
  }, '所选资料已导出，请妥善保存。')
}
async function previewInvalid() {
  try {
    invalid.value = await reloginAPI.invalid()
    dialog.value = 'invalid'
  } catch (e) {
    error.value = workspaceError(e)
  }
}
async function deleteInvalid() {
  if (!invalid.value) return
  if (
    await act(
      () => reloginAPI.removeInvalid(invalid.value!.revision),
      '已清理不再关联本站账号的登录资料。'
    )
  ) {
    selected.value = []
    closeDialog()
  }
}
async function deleteSelected() {
  if (await act(() => reloginAPI.remove(selected.value), '所选登录资料已删除。')) {
    selected.value = []
    closeDialog()
  }
}
async function clearHistory() {
  if (await act(() => reloginAPI.clearHistory(), '已完成的授权记录已清理。')) closeDialog()
}
async function showChallenge(run: LoginRun) {
  try {
    challenge.value = await reloginAPI.run(run.id)
    verifyCode.value = ''
    dialog.value = 'challenge'
  } catch (e) {
    error.value = workspaceError(e)
  }
}
async function completeChallenge() {
  if (!challenge.value) return
  const phone = challenge.value.challenge?.type?.includes('phone')
  if (
    await act(
      () =>
        reloginAPI.resume(
          challenge.value!.id,
          phone
            ? {
                phone_code: verifyCode.value,
                phone_number: phoneNumber.value,
                phone_channel: challenge.value?.challenge?.phone_channel
              }
            : { email_code: verifyCode.value }
        ),
      '验证已提交，正在等待授权结果。'
    )
  )
    closeDialog()
}
</script>

<template>
  <WorkspaceShell wide class="workspace-relogin">
    <div class="ws-toolbar">
      <div>
        <div class="relogin-summary">
          <span
            ><strong>{{ accounts.length }}</strong> 个 OAuth 账号</span
          ><span
            ><strong>{{ accounts.filter((account) => account.status === 'error').length }}</strong>
            个需要处理</span
          ><span v-if="activeRuns.length"
            ><strong>{{ activeRuns.length }}</strong> 个进行中的任务</span
          ><span
            v-if="settings"
            class="ws-label"
            :class="{ 'ws-label-active': settings.settings.auto_enabled }"
            >自动重登{{ settings.settings.auto_enabled ? '已开启' : '未开启' }}</span
          >
        </div>
      </div>
      <div class="ws-actions">
        <button
          class="ws-icon-btn"
          :disabled="loading"
          aria-label="刷新账号状态"
          @click="refresh()"
        >
          <Icon
            name="refresh"
            size="sm"
            :class="{ 'ws-spin': loading }"
          /></button
        ><button
          class="ws-button"
          :disabled="!settings || busy"
          @click="error = ''; dialog = 'settings'"
        >
          <Icon
            name="cog"
            size="sm"
          />设置</button
        ><button
          class="ws-button"
          @click="dialog = 'rt'"
        >
          RT 导入</button
        ><button
          class="ws-button ws-button-primary"
          @click="openLogin(null)"
        >
          <Icon
            name="plus"
            size="sm"
          />账号授权
        </button>
      </div>
    </div>
    <div
      v-if="error"
      class="ws-error"
      role="alert"
    >
      {{ error }}<button @click="error = ''">关闭</button>
    </div>
    <div
      v-if="notice"
      class="relogin-notice"
      role="status"
    >
      <Icon
        name="checkCircle"
        size="sm"
      />{{ notice }}<button @click="notice = ''">关闭</button>
    </div>
    <div
      class="ws-tabs"
      role="tablist"
      aria-label="失效重登功能"
    >
      <button
        v-for="item in tabs"
        :key="item.value"
        class="ws-tab"
        role="tab"
        :aria-selected="tab === item.value"
        @click="tab = item.value"
      >
        {{ item.label
        }}<span
          v-if="item.value === 'imported' && imported"
          class="ml-2 text-xs"
          >{{ imported.total }}</span
        >
      </button>
    </div>
    <div
      v-if="tab === 'accounts'"
      class="ws-toolbar"
    >
      <div class="relogin-filters">
        <input
          v-model="search"
          class="ws-input"
          placeholder="搜索邮箱或账号名称"
          aria-label="搜索渠道账号"
        /><select
          v-model="status"
          class="ws-input"
          aria-label="筛选渠道账号状态"
        >
          <option value="">全部状态</option>
          <option value="error">需要处理</option>
          <option value="active">正常</option>
          <option value="inactive">已暂停</option>
        </select>
      </div>
      <div class="ws-actions">
        <router-link
          class="ws-button ws-button-quiet"
          to="/admin/accounts"
          >渠道管理<Icon
            name="arrowRight"
            size="xs" /></router-link
        ><button
          class="ws-button"
          :disabled="busy || loading"
          @click="
            act(async () => {
              const result = await reloginAPI.auto()
              notice = `已提交 ${result.queued} 个符合条件的重登任务。`
            }, '已检查并提交符合条件的重登任务。')
          "
        >
          恢复失效账号
        </button>
      </div>
    </div>
    <div
      v-else-if="tab === 'imported'"
      class="ws-toolbar"
    >
      <input
        v-model="importedQuery"
        class="ws-input max-w-xs"
        placeholder="搜索保存的邮箱"
        aria-label="搜索登录资料"
      />
      <div class="ws-actions">
        <button
          class="ws-button ws-button-quiet"
          :disabled="busy"
          @click="previewInvalid"
        >
          预览失效资料</button
        ><button
          class="ws-button"
          :disabled="!selected.length || busy"
          @click="exportSelected"
        >
          导出所选</button
        ><button
          class="ws-button ws-button-danger"
          :disabled="!selected.length || busy"
          @click="dialog = 'delete'"
        >
          删除所选</button>
      </div>
    </div>
    <div
      v-else
      class="ws-toolbar"
    >
      <p>保留最近 200 条已完成记录，进行中的任务会持续更新。</p>
      <button
        class="ws-button ws-button-quiet"
        :disabled="busy || !history.length"
        @click="dialog = 'clear'"
      >
        清理已完成记录
      </button>
    </div>
    <div class="ws-panel">
      <div
        v-if="loading && !accounts.length && !imported"
        class="ws-empty"
        role="status"
      >
        <Icon
          name="refresh"
          class="ws-spin"
        />
        <p>正在读取本站账号…</p>
      </div>
      <ReloginAccounts
        v-else-if="tab === 'accounts'"
        :accounts="visibleAccounts"
        :runs="history"
        :busy="busy"
        @saved="startSaved"
        @login="openLogin"
      /><ReloginImported
        v-else-if="tab === 'imported'"
        v-model:selected="selected"
        :result="imported"
        @page="importedPage = $event"
      /><ReloginHistory
        v-else
        :runs="history"
        :busy="busy"
        @verify="showChallenge"
        @export="openExport"
        @apply="(run) => act(() => reloginAPI.apply(run.id), '授权结果已写回账号。')"
      />
    </div>
    <div
      v-if="settings?.quota"
      class="quota-footer"
    >
      <span
        >额度合计 <strong>{{ settings.quota.total_remaining_percent.toFixed(1) }}%</strong></span
      ><span>检查于 {{ formatTime(settings.quota.updated_at) }}</span>
    </div>
    <BaseDialog
      :show="dialog === 'login'"
      title="账号授权"
      content-class="workspace-relogin"
      :show-close-button="!busy"
      :close-on-escape="!busy"
      @close="closeDialog"
      ><div class="workspace"><ReloginAuthorizationForm
        v-if="dialog === 'login'"
        :account="loginAccount"
        :busy="busy"
        :error="error"
        :progress="importProgress"
        @start="start"
        @import="importSecrets"
    /></div></BaseDialog>
    <BaseDialog
      :show="dialog === 'settings'"
      title="失效重登设置"
      width="wide"
      content-class="workspace-relogin"
      :show-close-button="!busy"
      :close-on-escape="!busy"
      @close="closeDialog"
      ><div class="workspace"><ReloginSettingsForm
        v-if="dialog === 'settings' && settings"
        :settings="settings.settings"
        :options="options"
        :busy="busy"
        :error="error"
        @save="saveSettings"
    /></div>
      <template #footer>
        <div class="workspace workspace-relogin-footer">
          <button class="ws-button" type="button" :disabled="busy" @click="closeDialog">取消</button>
          <button class="ws-button ws-button-primary" type="submit" form="relogin-settings-form" :disabled="busy">
            {{ busy ? '保存中…' : '保存设置' }}
          </button>
        </div>
      </template>
    </BaseDialog>
    <WorkspaceDialog
      :open="dialog === 'rt'"
      title="Refresh Token 导入"
      @update:open="closeDialog"
      ><form
        class="space-y-4"
        @submit.prevent="importRT"
      >
        <p class="ws-muted">
          通过本站已有授权流程验证。相同邮箱会更新现有账号；没有对应账号时使用默认设置新建。
        </p>
        <label class="ws-field"
          >Refresh Token<textarea
            v-model="refreshToken"
            class="ws-input font-mono"
            rows="5"
            spellcheck="false"
            autocomplete="off"
            required
            placeholder="粘贴完整的 Refresh Token"
          />
        </label>
        <div class="ws-actions justify-end">
          <button
            class="ws-button ws-button-primary"
            :disabled="busy"
          >
            {{ busy ? '验证中，请稍候…' : '验证并写回' }}
          </button>
        </div>
      </form></WorkspaceDialog
    >
    <WorkspaceDialog
      :open="dialog === 'challenge'"
      title="完成额外验证"
      @update:open="closeDialog"
      ><form
        v-if="challenge"
        class="space-y-4"
        @submit.prevent="completeChallenge"
      >
        <p class="ws-muted">
          {{ challenge.email }} · {{ challenge.challenge?.message || '请填写账号收到的验证码。' }}
        </p>
        <label
          v-if="challenge.challenge?.type?.includes('phone')"
          class="ws-field"
          >手机号<input
            v-model="phoneNumber"
            class="ws-input"
            type="tel" /></label
        ><label class="ws-field"
          >验证码<input
            v-model="verifyCode"
            class="ws-input"
            inputmode="numeric"
            autocomplete="one-time-code"
            required
        /></label>
        <div class="ws-actions justify-end">
          <button
            class="ws-button ws-button-primary"
            :disabled="busy"
          >
            提交验证
          </button>
        </div>
      </form></WorkspaceDialog
    >
    <WorkspaceDialog
      :open="dialog === 'invalid'"
      title="预览不再关联的登录资料"
      @update:open="closeDialog"
      ><p class="ws-muted mb-4">
        仅检查邮箱是否仍存在于本站完整的 OpenAI OAuth 账号列表。账号暂停或报错不属于删除条件。
      </p>
      <div
        v-if="invalid?.items.length"
        class="invalid-list"
      >
        <p
          v-for="item in invalid.items"
          :key="item.email"
        >
          {{ item.email }}
        </p>
      </div>
      <p
        v-else
        class="text-sm"
      >
        当前没有需要清理的资料。
      </p>
      <template #footer
        ><button
          class="ws-button"
          @click="closeDialog"
        >
          返回</button
        ><button
          class="ws-button ws-button-danger"
          :disabled="busy || !invalid?.items.length"
          @click="deleteInvalid"
        >
          确认删除 {{ invalid?.items.length || 0 }} 条资料
        </button></template
      ></WorkspaceDialog
    >
    <WorkspaceDialog
      :open="dialog === 'delete'"
      title="删除所选登录资料"
      @update:open="closeDialog"
      ><p class="text-sm leading-7">
        将删除 {{ selected.length }} 条保存的密码和验证资料。此操作不会删除渠道账号。
      </p>
      <template #footer
        ><button
          class="ws-button"
          @click="closeDialog"
        >
          取消</button
        ><button
          class="ws-button ws-button-danger"
          :disabled="busy"
          @click="deleteSelected"
        >
          确认删除资料
        </button></template
      ></WorkspaceDialog
    >
    <WorkspaceDialog
      :open="dialog === 'clear'"
      title="清理授权记录"
      @update:open="closeDialog"
      ><p class="text-sm leading-7">
        清理已完成的历史记录和其中保存的授权结果。进行中及等待验证的任务会保留。
      </p>
      <template #footer
        ><button
          class="ws-button"
          @click="closeDialog"
        >
          取消</button
        ><button
          class="ws-button ws-button-danger"
          :disabled="busy"
          @click="clearHistory"
        >
          确认清理
        </button></template
      ></WorkspaceDialog
    >
    <WorkspaceDialog :open="dialog === 'export'" title="导出授权凭据" @update:open="closeDialog">
      <p class="ws-muted mb-4">{{ credentialRun?.email }} · 导出文件包含可用于登录的凭据，请妥善保管。</p>
      <label class="ws-field">目标格式
        <select v-model="exportFormat" class="ws-input">
          <option v-for="format in exportFormats" :key="format[0]" :value="format[0]">{{ format[1] }}</option>
        </select>
      </label>
      <template #footer>
        <button class="ws-button" :disabled="busy" @click="exportCredential(true)">复制当前格式</button>
        <button class="ws-button ws-button-primary" :disabled="busy" @click="exportCredential(false)">下载文件</button>
      </template>
    </WorkspaceDialog>
    <TotpStepUpDialog :controller="stepUp" />
  </WorkspaceShell>
</template>

<style scoped>
.relogin-summary {
  display: flex;
  align-items: center;
  gap: 20px;
  flex-wrap: wrap;
  margin-top: 12px;
  font-size: 12px;
  color: var(--ws-muted);
}
.relogin-summary strong {
  color: var(--ws-text);
  font-weight: 600;
}
.relogin-filters {
  display: flex;
  gap: 10px;
}
.relogin-filters input {
  width: 290px;
}
.relogin-filters select {
  width: 145px;
}
.relogin-notice {
  display: flex;
  align-items: center;
  gap: 9px;
  color: var(--ws-primary);
  background: color-mix(in srgb, var(--ws-primary) 7%, var(--ws-surface));
  border-radius: 9px;
  padding: 12px 16px;
  margin-bottom: 16px;
  font-size: 13px;
}
.relogin-notice button {
  margin-left: auto;
}
.quota-footer {
  display: flex;
  justify-content: space-between;
  gap: 15px;
  color: var(--ws-muted);
  font-size: 12px;
  margin-top: 18px;
}
.invalid-list {
  max-height: 300px;
  overflow: auto;
  font-size: 13px;
  line-height: 2;
  background: var(--ws-soft);
  padding: 12px;
  border-radius: 8px;
}
@media (max-width: 767px) {
  .relogin-filters {
    width: 100%;
  }
  .relogin-filters input {
    width: 100%;
  }
  .relogin-summary {
    gap: 12px;
  }
  .relogin-filters select {
    width: 115px;
  }
}
</style>
