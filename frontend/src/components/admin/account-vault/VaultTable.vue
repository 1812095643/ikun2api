<script setup lang="ts">
import { computed } from 'vue'
import DataTable from '@/components/common/DataTable.vue'
import VaultSecretCell from './VaultSecretCell.vue'
import { vaultFields, type VaultRecord, type VaultField } from '@/api/accountVault'
import { useClipboard } from '@/composables/useClipboard'
import { useAppStore } from '@/stores/app'
const props = defineProps<{ items: VaultRecord[]; loading: boolean; selected: Array<string | number>; fields: VaultField[]; read: (id: string, field: string) => Promise<string> }>()
const emit = defineEmits<{ 'update:selected': [value: Array<string | number>]; edit: [row: VaultRecord]; delete: [row: VaultRecord] }>()
const columns = computed(() => [{ key: 'title', label: '资料' }, ...vaultFields.filter(([key]) => props.fields.includes(key)).map(([key, label]) => ({ key, label })), { key: 'updated_at', label: '更新于' }, { key: 'actions', label: '操作' }])
const app = useAppStore(), { copyToClipboard } = useClipboard()
async function copy(text: string) { if (text && await copyToClipboard(text)) app.showSuccess('已复制') }
const plainFields = ['email', 'auxiliary_email']
const slots = vaultFields.map(([key]) => ({ key, slot: 'cell-' + key }))
</script>
<template>
 <DataTable :columns="columns" :data="items" :loading="loading" row-key="id" selectable :selected-keys="selected" :expandable-actions="false" @update:selected-keys="emit('update:selected', $event)">
  <template #cell-title="{ row }"><div class="min-w-[120px] max-w-48"><button class="block truncate font-medium text-gray-900 dark:text-gray-100" @click="copy(row.title || row.platform)">{{ row.title || row.platform }}</button><button class="mt-1 block text-xs text-gray-500" @click="copy(row.tags)">{{ row.tags || '无标签' }}</button><span class="mt-1 block text-xs text-gray-400">{{ row.status === 'active' ? '使用中' : row.status === 'archived' ? '已归档' : '未使用' }}</span></div></template>
  <template v-for="field in slots" :key="field.key" #[field.slot]="{ row }">
   <button v-if="plainFields.includes(field.key)" class="block max-w-[240px] truncate text-left text-sm text-gray-700 hover:text-primary-600 dark:text-gray-200" :aria-label="'复制 ' + field.key" @click="copy(row[field.key])">{{ row[field.key] || '—' }}</button>
   <VaultSecretCell v-else :id="row.id" :field="field.key" :present="row.fields[field.key]" :read="read" />
  </template>
  <template #cell-updated_at="{ row }"><button class="whitespace-nowrap text-xs text-gray-500" @click="copy(row.updated_at)">{{ new Date(row.updated_at).toLocaleString() }}</button></template>
  <template #cell-actions="{ row }"><div class="flex gap-2"><button class="btn btn-secondary btn-sm" @click="emit('edit', row)">编辑</button><button class="btn btn-ghost btn-sm text-red-600 dark:text-red-400" @click="emit('delete', row)">删除</button></div></template>
 </DataTable>
</template>
