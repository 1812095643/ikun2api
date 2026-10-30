<script setup lang="ts">
import { ref } from 'vue'
import { parseReloginImport } from '@/utils/reloginImport'
import type { LoginSecret } from '@/api/relogin'
const props = defineProps<{ busy: boolean; accountEmail?: string }>()
const emit = defineEmits<{ import: [items: LoginSecret[], authorize: boolean, apply: boolean] }>()
const input = ref(''),
  error = ref(''),
  apply = ref(true)
function submit(authorize = true) {
  if (props.busy) return
  error.value = ''
  try {
    emit('import', parseReloginImport(input.value, props.accountEmail), authorize, apply.value)
  } catch (e) {
    error.value = e instanceof Error ? e.message : '请检查账号资料格式后重试。'
  }
}
async function fileChanged(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file) return
  if (file.size > 2 * 1024 * 1024) {
    error.value = '文件超过 2 MB，请拆分后导入。'
    return
  }
  try {
    input.value = await file.text()
    error.value = ''
  } catch {
    error.value = '文件未能读取，请重新选择或直接粘贴资料。'
  } finally {
    (event.target as HTMLInputElement).value = ''
  }
}
</script>
<template>
  <form
    class="space-y-4"
    @submit.prevent="submit()"
  >
    <p v-if="accountEmail" class="ws-muted">当前账号：{{ accountEmail }}</p>
    <label class="ws-field"
      >账号资料<textarea
        v-model="input"
        class="ws-input font-mono"
        rows="6"
        :disabled="busy"
        :maxlength="2 * 1024 * 1024"
        spellcheck="false"
        autocomplete="off"
        placeholder="邮箱----密码----2FA 密钥&#10;每行一条，2FA 密钥可省略"
      /><small>支持直接粘贴，或选择 JSON / TXT 文件；每次最多 100 条。</small></label
    ><input
      type="file"
      accept=".json,.txt"
      class="ws-file"
      aria-label="选择资料文件"
      :disabled="busy"
      @change="fileChanged"
    />
    <label class="flex items-center gap-2 text-sm">
      <input v-model="apply" type="checkbox" :disabled="busy" />验证成功后写回本站账号
    </label>
    <p class="ws-muted">资料在本站加密保存，授权沿用原有验证服务。</p>
    <p
      v-if="error"
      class="ws-error"
      role="alert"
    >
      {{ error }}
    </p>
    <div class="ws-actions justify-end">
      <button type="button" class="ws-button" :disabled="busy" @click="submit(false)">仅保存资料</button>
      <button
        class="ws-button ws-button-primary"
        :disabled="busy"
      >
        {{ busy ? '提交中…' : '导入并授权' }}
      </button>
    </div>
  </form>
</template>
