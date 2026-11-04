<script setup lang="ts">
import { reactive, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import { emptyVaultEntry, vaultFields, type VaultEntry } from '@/api/accountVault'
const props = defineProps<{ show: boolean; entry: VaultEntry | null; saving: boolean }>()
const emit = defineEmits<{ close: []; save: [value: VaultEntry] }>()
const form = reactive(emptyVaultEntry())
watch(() => [props.show, props.entry] as const, () => { Object.assign(form, emptyVaultEntry(), props.entry ?? {}) }, { immediate: true })
const states = [{ value: 'unused', label: '未使用' }, { value: 'active', label: '使用中' }, { value: 'archived', label: '已归档' }]
</script>
<template>
 <BaseDialog :show="show" :title="entry?.existing_id ? '编辑资料' : '新增资料'" width="wide" @close="emit('close')">
  <form class="grid gap-4 sm:grid-cols-2" autocomplete="off" @submit.prevent="emit('save', { ...form, account_id: form.account_id || null })">
   <label class="text-sm">平台<input v-model="form.platform" class="input mt-1" maxlength="32" required /></label>
   <label class="text-sm">名称<input v-model="form.title" class="input mt-1" maxlength="200" /></label>
   <label class="text-sm">标签<input v-model="form.tags" class="input mt-1" maxlength="200" /></label>
   <label class="text-sm">状态<Select v-model="form.status" :options="states" class="mt-1" /></label>
   <label v-for="[key, label] in vaultFields" :key="key" class="min-w-0 text-sm" :class="{ 'sm:col-span-2': key === 'notes' || key === 'session_key' }">{{ label }}
    <textarea v-if="key === 'notes'" v-model="form[key]" class="input mt-1 min-h-20" maxlength="16000" />
    <input v-else v-model="form[key]" :type="['password','auxiliary_password','session_key','api_key'].includes(key) ? 'password' : 'text'" :required="key === 'email'" class="input mt-1 font-mono" spellcheck="false" autocomplete="new-password" :maxlength="key.includes('url') ? 2048 : 8192" />
   </label>
   <label class="text-sm">关联账号 ID（可选）<input v-model.number="form.account_id" type="number" min="1" class="input mt-1" /></label>
   <div class="flex items-end justify-end gap-2 sm:col-span-2"><button type="button" class="btn btn-secondary" @click="emit('close')">取消</button><button type="submit" class="btn btn-primary" :disabled="saving">{{ saving ? '保存中…' : '保存' }}</button></div>
  </form>
 </BaseDialog>
</template>
