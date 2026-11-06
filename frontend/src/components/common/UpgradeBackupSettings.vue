<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { configureUpgradeBackups, getUpgradeBackups, type UpgradeBackupStatus } from '@/api/admin/system'
import { formatBytes } from '@/utils/format'

const props = defineProps<{ disabled?: boolean }>()
const status = ref<UpgradeBackupStatus | null>(null)
const autoCleanup = ref(true)
const keep = ref(3)
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const saved = ref(false)
let disposed = false
onUnmounted(() => { disposed = true })
const locked = computed(() => props.disabled || loading.value || saving.value || !status.value)
const valid = computed(() => Number.isInteger(keep.value) && keep.value >= 1 && keep.value <= 30)

function errorMessage(value: unknown): string {
  const failure = value as { response?: { data?: { message?: string } }; message?: string }
  return failure.response?.data?.message || failure.message || '读取备份设置失败'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const result = await getUpgradeBackups()
    if (disposed) return
    status.value = result
    autoCleanup.value = result.policy.auto_cleanup
    keep.value = result.policy.keep
  } catch (value) {
    if (!disposed) error.value = errorMessage(value)
  } finally {
    if (!disposed) loading.value = false
  }
}

async function save() {
  if (locked.value || !valid.value) return
  saving.value = true
  saved.value = false
  error.value = ''
  try {
    const result = await configureUpgradeBackups({ auto_cleanup: autoCleanup.value, keep: keep.value })
    if (disposed) return
    status.value = result
    saved.value = true
  } catch (value) {
    if (!disposed) error.value = errorMessage(value)
  } finally {
    if (!disposed) saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <section class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-600" aria-label="升级备份">
    <div class="flex items-center justify-between gap-3">
      <h3 class="text-sm font-medium text-gray-900 dark:text-white">升级备份</h3>
      <span v-if="status" class="text-xs text-gray-500 dark:text-dark-400">{{ status.count }} 份 · {{ formatBytes(status.bytes) }}</span>
    </div>
    <p v-if="loading" role="status" class="text-sm text-gray-500 dark:text-dark-400">正在读取…</p>
    <form v-else-if="status" class="space-y-3" @submit.prevent="save">
      <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
        <input v-model="autoCleanup" type="checkbox" role="switch" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" :disabled="locked" @change="saved = false" />
        自动清理旧备份
      </label>
      <div class="flex flex-wrap items-center gap-3">
        <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
          保留份数
          <input v-model.number="keep" type="number" min="1" max="30" step="1" required class="input w-20" :disabled="locked" @input="saved = false" />
        </label>
        <button type="submit" class="btn btn-secondary btn-sm" :disabled="locked || !valid">{{ saving ? '保存中…' : '保存' }}</button>
        <span v-if="saved" role="status" class="text-xs text-emerald-600 dark:text-emerald-400">已保存</span>
      </div>
      <p v-if="status.protected_count" class="text-xs text-gray-500 dark:text-dark-400">受保护 {{ status.protected_count }} 份</p>
    </form>
    <div v-if="error" class="flex items-center gap-3">
      <p role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <button v-if="!status" type="button" class="btn btn-secondary btn-sm" :disabled="disabled || loading" @click="load">重试</button>
    </div>
  </section>
</template>