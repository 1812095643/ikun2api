<script setup lang="ts">
import { ref, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { LoginSecret } from '@/api/relogin'
import { workspaceError } from '@/api/workspace'

const props = defineProps<{ secret: LoginSecret | null; email?: string; busy: boolean; error?: string }>()
const emit = defineEmits<{ close: []; save: [secret: LoginSecret] }>()
const draft = ref<LoginSecret | null>(null)
const copied = ref('')
const copyError = ref('')
watch(() => props.secret, value => {
  draft.value = value ? { ...value } : null
  copied.value = ''
  copyError.value = ''
}, { immediate: true })
async function copy(value: string) {
  try { await navigator.clipboard.writeText(value); copied.value = '已复制'; copyError.value = '' }
  catch (error) { copyError.value = workspaceError(error) }
}
function save() { if (draft.value && !props.busy) emit('save', { ...draft.value }) }
</script>
<template>
  <BaseDialog :show="Boolean(secret)" :title="`账号资料 · ${secret?.email || email || ''}`" :show-close-button="!busy" :close-on-escape="!busy" @close="emit('close')">
    <form v-if="draft" id="relogin-secret-form" class="workspace space-y-4" autocomplete="off" spellcheck="false" @submit.prevent="save">
      <p v-if="error || copyError" class="ws-error" role="alert">{{ error || copyError }}</p>
      <p v-if="copied" class="text-sm text-emerald-600" role="status">{{ copied }}</p>
      <fieldset :disabled="busy" class="space-y-4">
        <label class="ws-field">邮箱<div class="flex gap-2"><input :value="draft.email" class="ws-input flex-1" readonly aria-label="资料邮箱" /><button type="button" class="ws-button" @click="copy(draft.email)">复制</button></div></label>
        <label class="ws-field">登录方式<select v-model="draft.auth_mode" class="ws-input" aria-label="资料登录方式"><option value="password_2fa">密码 / MFA</option><option value="email_otp">邮箱验证码</option></select></label>
        <label class="ws-field">密码<div class="flex gap-2"><input v-model="draft.password" class="ws-input flex-1 font-mono" autocomplete="off" aria-label="资料密码" /><button type="button" class="ws-button" :disabled="!draft.password" @click="copy(draft.password || '')">复制</button></div></label>
        <label class="ws-field">MFA 密钥<div class="flex gap-2"><input v-model="draft.mfa_secret" class="ws-input flex-1 font-mono" autocomplete="off" aria-label="资料MFA密钥" /><button type="button" class="ws-button" :disabled="!draft.mfa_secret" @click="copy(draft.mfa_secret || '')">复制</button></div></label>
        <label class="ws-field">收件箱地址<div class="flex gap-2"><input v-model="draft.inbox_url" class="ws-input flex-1" autocomplete="off" aria-label="资料收件箱地址" /><button type="button" class="ws-button" :disabled="!draft.inbox_url" @click="copy(draft.inbox_url || '')">复制</button></div></label>
      </fieldset>
    </form>
    <template #footer>
      <button class="ws-button" :disabled="busy" @click="emit('close')">关闭</button>
      <button v-if="draft" type="button" class="ws-button" :disabled="busy" @click="copy([draft.email, draft.password || '', draft.mfa_secret || draft.mfa_code || '', ...(draft.inbox_url ? [draft.inbox_url] : [])].join('----'))">复制整行</button>
      <button class="ws-button ws-button-primary" type="submit" form="relogin-secret-form" :disabled="busy">保存资料</button>
    </template>
  </BaseDialog>
</template>
