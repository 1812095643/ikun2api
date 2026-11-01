<script setup lang="ts">
import { watch, computed } from 'vue'
import BaseDialog from './BaseDialog.vue'
import { useSystemUpdate } from '@/composables/useSystemUpdate'
const props = defineProps<{ show: boolean; version?: string }>()
const emit = defineEmits<{ close: [] }>()
const { info, phase, error, needRestart, busy, check, update, restart } = useSystemUpdate()
const current = computed(() => info.value?.current_version || props.version || '—')
const progress = computed(() => ({ checking: '正在检查更新…', draining: '正在等待当前请求完成…', updating: '正在下载、校验并备份数据，请稍候…', restarting: '更新已安装，正在重启服务…', failed: '', idle: '' }[phase.value]))
watch(() => props.show, show => { if (show && !info.value) void check() })
</script>

<template>
  <BaseDialog :show="show" title="版本与更新" width="normal" :close-on-escape="!busy" :show-close-button="!busy" @close="emit('close')">
    <div class="space-y-5">
      <div class="flex items-center justify-between gap-4">
        <div><p class="text-sm text-gray-500 dark:text-dark-400">当前版本</p><p class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ current }}</p></div>
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="check(true)">检查更新</button>
      </div>
      <div v-if="info?.has_update" class="rounded-xl border border-primary-200 bg-primary-50 p-4 dark:border-primary-800 dark:bg-primary-900/20">
        <p class="font-medium text-gray-900 dark:text-white">可更新至 {{ info.latest_version }}</p>
        <p class="mt-1 text-sm text-gray-600 dark:text-dark-300">升级前自动备份数据库和应用资料，保留现有账号、余额、密钥及设置。</p>
      </div>
      <p v-else-if="info && !info.warning" class="text-sm text-gray-600 dark:text-dark-300">已是最新版本。</p>
      <p v-if="info && !info.supports_self_update" class="text-sm text-gray-500 dark:text-dark-400">当前启动方式不支持自动更新，请使用迁入脚本启用受监护启动。</p>
      <p v-if="info?.warning" class="text-sm text-amber-700 dark:text-amber-400">{{ info.warning }}</p>
      <pre v-if="info?.release_info?.body" class="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-xl bg-gray-50 p-4 font-sans text-sm leading-6 text-gray-700 dark:bg-dark-900 dark:text-dark-300">{{ info.release_info.body }}</pre>
      <p v-if="progress" role="status" class="text-sm text-primary-600 dark:text-primary-400">{{ progress }}</p>
      <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-400">{{ error }}</p>
    </div>
    <template #footer>
      <button type="button" class="btn btn-secondary" :disabled="busy" @click="emit('close')">关闭</button>
      <button v-if="needRestart && !busy" type="button" class="btn btn-primary" @click="restart">重试重启</button>
      <button v-else-if="info?.has_update && info.supports_self_update" type="button" class="btn btn-primary" :disabled="busy" @click="update">{{ busy ? '正在更新…' : '备份并一键更新' }}</button>
    </template>
  </BaseDialog>
</template>
