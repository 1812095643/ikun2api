<script setup lang="ts">
import { shallowRef, ref } from 'vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import Select from '@/components/common/Select.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import VaultTable from './VaultTable.vue'
import VaultImportDialog from './VaultImportDialog.vue'
import VaultEditDialog from './VaultEditDialog.vue'
import VaultSecurityToggle from './VaultSecurityToggle.vue'
import { useAccountVault } from '@/composables/useAccountVault'
import { accountVaultAPI, emptyVaultEntry, vaultFields, type VaultEntry, type VaultRecord, type VaultField } from '@/api/accountVault'
const { items, selected, query, platform, status, page, pageSize, total, loading, busy, error, stepUp, protect, read, save, remove, load } = useAccountVault()
const importing = shallowRef(false), editing = shallowRef(false), editID = shallowRef<string | null>(null)
const draft = ref<VaultEntry | null>(null), deleteIDs = ref<string[]>([])
const fields = ref<VaultField[]>(['email', 'password', 'auxiliary_email', 'auxiliary_password', 'session_key', 'api_key'])
async function edit(row?: VaultRecord) {
 try {
  if (row) { const result = await protect(() => accountVaultAPI.reveal(row.id, 'all')); draft.value = { ...emptyVaultEntry(), ...result.secret, platform: row.platform, title: row.title, tags: row.tags, status: row.status, account_id: row.account_id, existing_id: row.id, expected_version: result.version }; editID.value = row.id }
  else { draft.value = emptyVaultEntry(); editID.value = null }
  editing.value = true
 } catch { /* centralized sanitized error */ }
}
function closeEdit() { editing.value = false; draft.value = null }
async function onSave(value: VaultEntry) { if (await save(editID.value, value)) closeEdit() }
async function confirmDelete() { if (await remove(deleteIDs.value)) deleteIDs.value = [] }
function search() { if (page.value !== 1) page.value = 1; else load() }
</script>
<template>
 <TablePageLayout>
  <template #filters>
   <div class="mb-3 flex justify-end"><VaultSecurityToggle :protect="protect" /></div>
   <form class="flex flex-wrap items-center justify-between gap-3" @submit.prevent="search">
    <div class="flex flex-wrap items-center gap-2"><input v-model="platform" class="input w-28" aria-label="平台筛选" placeholder="平台" /><input v-model="query" class="input w-64" aria-label="搜索资料" placeholder="完整邮箱、名称、标签" /><Select v-model="status" class="w-32" :options="[{ value: '', label: '全部状态' }, { value: 'unused', label: '未使用' }, { value: 'active', label: '使用中' }, { value: 'archived', label: '已归档' }]" @update:model-value="search" /><button class="btn btn-secondary" type="submit">搜索</button><span class="text-xs text-gray-500">{{ total }} 条资料</span></div>
    <div class="flex flex-wrap items-center gap-2"><button class="btn btn-secondary" type="button" :disabled="loading" @click="load">刷新</button><details class="relative"><summary class="btn btn-secondary cursor-pointer">显示列</summary><div class="absolute right-0 z-20 mt-2 w-44 space-y-2 rounded-xl border border-gray-200 bg-white p-3 shadow-lg dark:border-dark-700 dark:bg-dark-800"><label v-for="[key, label] in vaultFields" :key="key" class="flex gap-2 text-sm"><input v-model="fields" type="checkbox" :value="key" />{{ label }}</label></div></details><button v-if="selected.length" class="btn btn-secondary text-red-600" type="button" @click="deleteIDs = selected.map(String)">删除所选 {{ selected.length }} 条</button><button class="btn btn-secondary" type="button" @click="edit()">新增</button><button class="btn btn-primary" type="button" @click="importing = true">导入资料</button></div>
   </form>
   <p v-if="error" class="mt-3 text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</p>
  </template>
  <template #table><VaultTable :items="items" :loading="loading" v-model:selected="selected" :fields="fields" :read="read" @edit="edit" @delete="deleteIDs = [$event.id]" /></template>
  <template #pagination><Pagination v-model:page="page" v-model:page-size="pageSize" :total="total" /></template>
 </TablePageLayout>
 <VaultImportDialog v-if="importing" :show="importing" :protect="protect" @close="importing = false" @saved="load" />
 <VaultEditDialog v-if="editing" :show="editing" :entry="draft" :saving="busy" @close="closeEdit" @save="onSave" />
 <BaseDialog :show="!!deleteIDs.length" title="删除资料" @close="deleteIDs = []"><p>确认删除 {{ deleteIDs.length }} 条资料？该操作只移除资料库记录。</p><template #footer><button class="btn btn-secondary" :disabled="busy" @click="deleteIDs = []">取消</button><button class="btn btn-primary" :disabled="busy" @click="confirmDelete">确认删除</button></template></BaseDialog>
 <TotpStepUpDialog :controller="stepUp" />
</template>
