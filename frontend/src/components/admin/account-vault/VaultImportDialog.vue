<script setup lang="ts">
import { ref, shallowRef, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import { mapVaultColumns } from '@/utils/vaultImportMapping'
import { accountVaultAPI, vaultFields, type VaultEntry, type VaultParseRow } from '@/api/accountVault'
import { useAppStore } from '@/stores/app'
import { isStepUpCancelled } from '@/composables/useStepUp'
const props = defineProps<{ show: boolean; protect: <T>(fn: () => Promise<T>) => Promise<T> }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const app = useAppStore()
const raw = shallowRef(''), error = shallowRef(''), busy = shallowRef(false)
const problems = ref<VaultParseRow[]>([])
const mapped = shallowRef(false), delimiter = shallowRef('----')
const mapping = ref(['email', 'password', 'session_key', 'auxiliary_email', 'auxiliary_password'])
const mappingOptions = [{ value: '', label: '忽略此列' }, ...vaultFields.map(([value, label]) => ({ value, label }))]
const duplicateAction = shallowRef<'skip' | 'fill' | 'replace'>('skip')
const actions = [{ value: 'skip', label: '跳过已有邮箱（默认）' }, { value: 'fill', label: '仅补空字段' }, { value: 'replace', label: '更新非空凭据' }]
let batch = '', prepared: VaultEntry[] | null = null, preparedInput = ''
function enableMapping(value: boolean) {
  mapped.value = value
  const count = raw.value.split(/\r?\n/).find(s => s.trim())?.split(delimiter.value).length || 5
  mapping.value = Array.from({ length: Math.min(12, count) }, (_, i) => mapping.value[i] || '')
}
watch(() => props.show, () => {
  raw.value = ''; problems.value = []; error.value = ''; mapped.value = false; duplicateAction.value = 'skip'
  batch = crypto.randomUUID(); prepared = null; preparedInput = ''
}, { immediate: true })
async function save() {
  if (busy.value || !raw.value.trim()) return
  busy.value = true; error.value = ''; problems.value = []
  try {
    const text = mapped.value ? mapVaultColumns(raw.value, mapping.value, delimiter.value) : raw.value
    const input = JSON.stringify([text, duplicateAction.value])
    if (!prepared || preparedInput !== input) {
      const result = await props.protect(() => accountVaultAPI.preview(text))
      problems.value = result.rows.filter(row => !!row.error)
      if (problems.value.length) { error.value = `有 ${problems.value.length} 处无法可靠识别，尚未写入。请修改下方对应内容后重试。`; return }
      if (!result.rows.length) { error.value = '没有识别到账号资料，请粘贴完整内容。'; return }
      prepared = result.rows.map(row => ({ ...row.entry, action: row.entry.existing_id ? duplicateAction.value : 'insert' }))
      preparedInput = input; batch = crypto.randomUUID()
    }
    // Keep this exact batch and payload after an interrupted response, allowing
    // the server's idempotency record to safely resolve a retry.
    const result = await props.protect(() => accountVaultAPI.import(batch, prepared!))
    app.showSuccess(`导入完成：新增 ${result.inserted || 0} 条，更新 ${result.updated || 0} 条，跳过 ${result.skipped || 0} 条。`)
    raw.value = ''; prepared = null; emit('saved'); emit('close')
  } catch (e) {
    if ((e as { status?: number })?.status === 409) { prepared = null; preparedInput = '' }
    if (!isStepUpCancelled(e)) error.value = '导入未完成，请检查格式或网络后重试；重试不会重复写入同一批次。'
  } finally { busy.value = false }
}
async function fileInput(event: Event) {
  const input = event.target as HTMLInputElement, file = input.files?.[0]
  if (file) { if (file.size > 2 * 1024 * 1024) error.value = '文件超过 2 MiB，请分批导入。'; else raw.value = await file.text() }
  input.value = ''
}
</script>
<template>
  <BaseDialog :show="show" title="导入账号资料" width="wide" :show-close-button="!busy" :close-on-escape="!busy" @close="!busy && emit('close')">
    <form id="vault-quick-import" class="space-y-4" @submit.prevent="save">
      <p class="text-sm text-gray-500 dark:text-dark-400">粘贴多份资料，系统自动批量识别并加密保存。支持中文资料块、JSON、CSV / Excel，以及横线分隔格式；每批最多 500 条。</p>
      <textarea v-model="raw" :disabled="busy" class="input min-h-72 font-mono text-sm" aria-label="待导入资料" placeholder="将整段账号资料粘贴到这里，无需逐条整理。" autocomplete="off" spellcheck="false" />
      <div v-if="error" role="alert" class="rounded-xl bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ error }}<ul v-if="problems.length" class="mt-2 max-h-36 list-inside list-disc overflow-auto text-xs"><li v-for="row in problems" :key="row.line">第 {{ row.line }} 行：{{ row.error }}</li></ul></div>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <label class="btn btn-secondary cursor-pointer" :class="{ 'pointer-events-none opacity-50': busy }">选择文件<input type="file" accept=".txt,.csv,.json,.tsv" class="hidden" :disabled="busy" @change="fileInput" /></label>
        <span class="text-xs text-gray-500 dark:text-dark-400">已有邮箱默认跳过，凭据不自动覆盖。</span>
      </div>
      <details class="rounded-xl border border-gray-200 p-3 dark:border-dark-700">
        <summary class="cursor-pointer text-sm text-gray-600 dark:text-dark-300">更多导入选项</summary>
        <fieldset class="mt-4 space-y-4" :disabled="busy">
          <div><label id="vault-duplicate-label" class="input-label">遇到已有邮箱</label><Select v-model="duplicateAction" :options="actions" :disabled="busy" aria-labelledby="vault-duplicate-label" /></div>
          <label class="flex items-center gap-2 text-sm"><input type="checkbox" :checked="mapped" @change="enableMapping(($event.target as HTMLInputElement).checked)" />自定义分隔列顺序</label>
          <div v-if="mapped" class="space-y-3"><Select v-model="delimiter" :disabled="busy" :options="[{ value: '----', label: '四横线 ----' }, { value: '\t', label: '制表符（Excel 复制）' }]" @update:model-value="enableMapping(true)" /><div class="grid gap-2 sm:grid-cols-3"><label v-for="(_, index) in mapping" :key="index" class="text-xs text-gray-500">第 {{ index + 1 }} 列<Select v-model="mapping[index]" :options="mappingOptions" :disabled="busy" class="mt-1" /></label></div></div>
        </fieldset>
      </details>
    </form>
    <template #footer>
      <button type="button" class="btn btn-secondary" :disabled="busy" @click="emit('close')">取消</button>
      <button type="submit" form="vault-quick-import" class="btn btn-primary" :disabled="busy || !raw.trim()">{{ busy ? '识别并导入中…' : '确定导入' }}</button>
    </template>
  </BaseDialog>
</template>
