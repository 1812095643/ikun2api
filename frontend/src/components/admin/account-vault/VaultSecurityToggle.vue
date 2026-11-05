<script setup lang="ts">
import { onMounted, ref } from 'vue'
import Toggle from '@/components/common/Toggle.vue'
import { accountVaultAPI } from '@/api/accountVault'
import { isStepUpCancelled } from '@/composables/useStepUp'
import { useAppStore } from '@/stores/app'
const props=defineProps<{ protect:<T>(fn:()=>Promise<T>)=>Promise<T> }>()
const enabled=ref(false),ready=ref(false),busy=ref(false),error=ref('')
const app=useAppStore()
async function load(){
  busy.value=true;error.value=''
  try { enabled.value=(await accountVaultAPI.security()).two_factor_enabled;ready.value=true }
  catch { ready.value=false;error.value='保护设置读取失败' }
  finally { busy.value=false }
}
async function change(value:boolean){
  if(busy.value||!ready.value)return
  busy.value=true;error.value=''
  try { enabled.value=(await props.protect(()=>accountVaultAPI.saveSecurity(value))).two_factor_enabled;app.showSuccess(enabled.value?'资料库两步验证保护已开启':'资料库两步验证保护已关闭') }
  catch(e){if(!isStepUpCancelled(e))error.value='保护设置未保存，请重试'}
  finally {busy.value=false}
}
onMounted(load)
</script>
<template>
  <div class="flex flex-wrap items-center gap-2 text-sm text-gray-600 dark:text-dark-300">
    <Toggle :model-value="enabled" :disabled="busy || !ready" aria-label="资料库两步验证保护" @update:model-value="change" />
    <span title="仅控制资料库导入、查看凭据、编辑与删除的二次验证。开启前需配置并验证两步验证；关闭时也需验证。资料仍加密且仅管理员可访问。">两步验证保护</span>
    <span v-if="busy" class="text-xs text-gray-400">处理中…</span>
    <button v-if="error" type="button" class="text-xs text-red-600 underline dark:text-red-400" @click="load">{{ error }} · 重试</button>
  </div>
</template>
