<script setup lang="ts">
import { computed, ref, shallowRef, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import FormSteps from '@/components/common/FormSteps.vue'
import Select from '@/components/common/Select.vue'
import { mapVaultColumns } from '@/utils/vaultImportMapping'
import { accountVaultAPI, vaultFields, type VaultParseRow } from '@/api/accountVault'
const props = defineProps<{ show: boolean; protect: <T>(fn: () => Promise<T>) => Promise<T> }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const step = shallowRef(1), raw = shallowRef(''), error = shallowRef(''), busy = shallowRef(false)
const rows = ref<VaultParseRow[]>([])
const mapped = shallowRef(false), showSecrets = shallowRef(false), delimiter = shallowRef('----')
const mapping = ref(['email', 'password', 'session_key', 'auxiliary_email', 'auxiliary_password'])
const mappingOptions = [{ value: '', label: '忽略此列' }, ...vaultFields.map(([value, label]) => ({ value, label }))]
function enableMapping(value: boolean) {
 mapped.value = value
 const count = raw.value.split(/\r?\n/).find(s => s.trim())?.split(delimiter.value).length || 5
 mapping.value = Array.from({ length: Math.min(12, count) }, (_, i) => mapping.value[i] || '')
}
let batch = ''
const valid = computed(() => rows.value.length > 0 && rows.value.every(row => !row.error))
const counts = computed(() => ({ insert: rows.value.filter(row => !row.entry.existing_id).length, duplicates: rows.value.filter(row => row.entry.existing_id).length }))
const actions = [{ value: 'skip', label: '跳过' }, { value: 'fill', label: '仅补空字段' }, { value: 'replace', label: '更新非空凭据' }]
watch(() => props.show, () => { step.value = 1; raw.value = ''; rows.value = []; mapped.value = false; showSecrets.value = false; error.value = ''; batch = crypto.randomUUID() }, { immediate: true })
async function preview() {
 busy.value = true; error.value = ''
 try { const text = mapped.value ? mapVaultColumns(raw.value, mapping.value, delimiter.value) : raw.value; const result = await props.protect(() => accountVaultAPI.preview(text)); rows.value = result.rows; step.value = 2 }
 catch (e) { error.value = e instanceof Error ? e.message : '无法解析，请检查格式和两步验证。' } finally { busy.value = false }
}
async function save() {
 busy.value = true; error.value = ''
 try { await props.protect(() => accountVaultAPI.import(batch, rows.value.map(row => row.entry))); raw.value = ''; rows.value = []; emit('saved'); emit('close') }
 catch { error.value = '导入未完成，资料可能已变化，请重新预览。' } finally { busy.value = false }
}
async function fileInput(event: Event) {
 const input = event.target as HTMLInputElement, file = input.files?.[0]
 if (file) { if (file.size > 2 * 1024 * 1024) error.value = '文件超过 2 MiB，请分批导入。'; else raw.value = await file.text() }
 input.value = ''
}
</script>
<template>
 <BaseDialog :show="show" title="导入账号资料" width="extra-wide" @close="!busy && emit('close')">
  <FormSteps :steps="['粘贴资料', '检查字段', '确认保存']" :current="step" />
  <div v-if="error" role="alert" class="my-3 text-sm text-red-600 dark:text-red-400">{{ error }}</div>
  <div v-if="step === 1" class="mt-5 space-y-3">
   <p class="text-sm text-gray-500">支持多份中文资料、CSV、JSON，以及：邮箱----密码----SessionKey----辅助邮箱----辅助密码。</p>
   <textarea v-model="raw" class="input min-h-72 font-mono text-sm" aria-label="待导入资料" placeholder="将账号资料粘贴到这里，每行或每个资料块一条。" autocomplete="off" spellcheck="false" />
   <div class="space-y-3"><label class="flex items-center gap-2 text-sm"><input type="checkbox" :checked="mapped" @change="enableMapping(($event.target as HTMLInputElement).checked)" />自定义分隔列顺序</label><div v-if="mapped" class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-dark-700"><Select v-model="delimiter" :options="[{ value: '----', label: '四横线 ----' }, { value: '\t', label: '制表符（Excel 复制）' }]" @update:model-value="enableMapping(true)" /><div class="grid gap-2 sm:grid-cols-3"><label v-for="(_, index) in mapping" :key="index" class="text-xs text-gray-500">第 {{ index + 1 }} 列<Select v-model="mapping[index]" :options="mappingOptions" class="mt-1" /></label></div></div></div>
   <label class="btn btn-secondary inline-flex cursor-pointer">选择文件<input type="file" accept=".txt,.csv,.json,.tsv" class="hidden" @change="fileInput" /></label>
  </div>
  <div v-else-if="step === 2" class="mt-5 space-y-3">
   <p class="text-sm text-gray-500">{{ rows.length }} 条资料 · {{ counts.insert }} 条新增 · {{ counts.duplicates }} 条重复。凭据保持原值；有错误时请返回修正。</p>
   <label class="flex items-center gap-2 text-sm"><input v-model="showSecrets" type="checkbox" />显示凭据以核对字段</label>
   <div class="max-h-[45vh] overflow-auto rounded-lg border border-gray-200 dark:border-dark-700"><table class="w-full text-left text-sm"><thead class="sticky top-0 bg-gray-50 dark:bg-dark-800"><tr><th class="p-3">行</th><th class="p-3">邮箱</th><th class="p-3">辅助邮箱</th><th class="p-3">凭据</th><th class="p-3">处理</th></tr></thead><tbody><tr v-for="(row, index) in rows" :key="index" class="border-t border-gray-100 dark:border-dark-700"><td class="p-3">{{ row.line }}</td><td class="p-3">{{ row.entry.email || '—' }}<p v-if="row.error" class="mt-1 text-xs text-red-600 dark:text-red-400">{{ row.error }}</p></td><td class="p-3">{{ row.entry.auxiliary_email || '—' }}</td><td class="p-3 text-xs"><span>{{ row.entry.password ? '密码 ✓' : '无密码' }}</span><code v-if="showSecrets" class="mt-1 block max-w-64 break-all">{{ row.entry.password }}</code><span class="mt-1 block">{{ row.entry.session_key ? 'SessionKey ✓' : row.entry.api_key ? 'API Key ✓' : '无 Key' }}</span><code v-if="showSecrets" class="mt-1 block max-w-64 break-all">{{ row.entry.session_key || row.entry.api_key }}</code><code v-if="showSecrets && row.entry.auxiliary_password" class="mt-1 block max-w-64 break-all">辅邮密码：{{ row.entry.auxiliary_password }}</code></td><td class="p-3"><Select v-if="row.entry.existing_id" v-model="row.entry.action" :options="actions" /><span v-else>新增</span></td></tr></tbody></table></div>
  </div>
  <div v-else class="my-8 text-center"><p class="font-medium">确认保存 {{ rows.length }} 条资料？</p><p class="mt-2 text-sm text-gray-500">资料将加密入库，删除或编辑均保留操作记录。</p></div>
  <template #footer><div class="flex justify-end gap-2"><button class="btn btn-secondary" :disabled="busy" @click="step > 1 ? step-- : emit('close')">{{ step > 1 ? '上一步' : '取消' }}</button><button v-if="step === 1" class="btn btn-primary" :disabled="busy || !raw.trim()" @click="preview">{{ busy ? '解析中…' : '识别并预览' }}</button><button v-else-if="step === 2" class="btn btn-primary" :disabled="!valid" @click="step = 3">下一步</button><button v-else class="btn btn-primary" :disabled="busy" @click="save">{{ busy ? '导入中…' : '确认导入' }}</button></div></template>
 </BaseDialog>
</template>
