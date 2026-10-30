<script setup lang="ts">
import { reactive } from 'vue'
import type { LoginSecret, ReloginAccount } from '@/api/relogin'
const props = defineProps<{ account: ReloginAccount | null; busy: boolean }>()
const emit = defineEmits<{
  start: [payload: LoginSecret & { account_id?: number; apply: boolean }]
}>()
const draft = reactive<LoginSecret & { apply: boolean }>({
  email: props.account?.email || '',
  password: '',
  mfa_secret: '',
  mfa_code: '',
  inbox_url: '',
  auth_mode: 'password_2fa',
  apply: true
})
</script>
<template>
  <form
    class="space-y-4"
    @submit.prevent="emit('start', { ...draft, account_id: account?.id })"
  >
    <fieldset class="space-y-4" :disabled="busy">
    <label class="ws-field"
      >账号邮箱<input
        v-model="draft.email"
        type="email"
        required
        class="ws-input"
        :readonly="!!account"
        autocomplete="off" /></label
    ><label class="ws-field"
      >验证方式<select
        v-model="draft.auth_mode"
        class="ws-input"
      >
        <option value="password_2fa">密码 / 两步验证</option>
        <option value="email_otp">邮箱验证码</option>
      </select></label
    ><template v-if="draft.auth_mode === 'password_2fa'"
      ><label class="ws-field"
        >密码<input
          v-model="draft.password"
          type="password"
          required
          class="ws-input"
          autocomplete="new-password"
      /></label>
      <div class="ws-form-grid">
        <label class="ws-field"
          >MFA 密钥（选填）<input
            v-model="draft.mfa_secret"
            type="password"
            class="ws-input"
            autocomplete="off" /></label
        ><label class="ws-field"
          >当前验证码（选填）<input
            v-model="draft.mfa_code"
            class="ws-input"
            inputmode="numeric"
            maxlength="12"
            autocomplete="one-time-code"
        /></label></div></template
    ><label
      v-else
      class="ws-field"
      >收件箱地址（选填）<input
        v-model="draft.inbox_url"
        type="url"
        class="ws-input"
        placeholder="https://…"
      /><small>不填写时，在下一步手动输入邮箱验证码。</small></label
    ><label class="flex items-center gap-2 text-sm"
      ><input
        v-model="draft.apply"
        type="checkbox"
      />验证成功后写回本站账号</label
    >
    <p class="ws-muted">
      登录资料将发送至原有授权服务 session.ameng2027.xyz 完成验证。此表单不会保存密码。
    </p>
    </fieldset>
    <div class="ws-actions justify-end">
      <button
        class="ws-button ws-button-primary"
        :disabled="busy"
      >
        {{ busy ? '提交中…' : '开始授权' }}
      </button>
    </div>
  </form>
</template>
