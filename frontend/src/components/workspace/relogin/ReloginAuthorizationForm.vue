<script setup lang="ts">
import { ref } from 'vue'
import ReloginImportForm from './ReloginImportForm.vue'
import ReloginLoginForm from './ReloginLoginForm.vue'
import type { LoginSecret, ReloginAccount } from '@/api/relogin'

defineProps<{ account: ReloginAccount | null; busy: boolean; error: string; progress?: string }>()
const emit = defineEmits<{
  start: [payload: LoginSecret & { account_id?: number; apply: boolean }]
  import: [items: LoginSecret[], authorize: boolean, apply: boolean]
}>()
const mode = ref('paste')
const modes = [{ value: 'paste', label: '粘贴资料' }, { value: 'manual', label: '手动填写' }]
</script>

<template>
  <div>
    <div class="ws-tabs" role="tablist" aria-label="账号授权方式">
      <button v-for="item in modes" :id="`relogin-auth-tab-${item.value}`" :key="item.value"
        type="button" class="ws-tab" role="tab" :aria-selected="mode === item.value"
        :aria-controls="`relogin-auth-panel-${item.value}`" :disabled="busy" @click="mode = item.value">
        {{ item.label }}
      </button>
    </div>
    <p v-if="error" class="ws-error mb-4" role="alert">{{ error }}</p>
    <p v-if="progress" class="ws-muted mb-4" role="status">{{ progress }}</p>
    <div id="relogin-auth-panel-paste" v-show="mode === 'paste'" role="tabpanel" aria-labelledby="relogin-auth-tab-paste">
      <ReloginImportForm :account-email="account?.email" :busy="busy"
        @import="(items, authorize, apply) => emit('import', items, authorize, apply)" />
    </div>
    <div id="relogin-auth-panel-manual" v-show="mode === 'manual'" role="tabpanel" aria-labelledby="relogin-auth-tab-manual">
      <ReloginLoginForm :account="account" :busy="busy" @start="emit('start', $event)" />
    </div>
  </div>
</template>
