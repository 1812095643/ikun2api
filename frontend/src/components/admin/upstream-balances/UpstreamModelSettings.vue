<script setup lang="ts">
import { reactive, shallowRef, watch } from 'vue'
import Toggle from '@/components/common/Toggle.vue'
import type { ModelMonitorConfig } from '@/api/upstreamModels'

const props = defineProps<{ config: ModelMonitorConfig; busy: boolean }>()
const emit = defineEmits<{ save: [config: ModelMonitorConfig] }>()
const form = reactive<ModelMonitorConfig>({ enabled: false, interval_minutes: 30, notify: false, recipients: [] })
const recipients = shallowRef('')
const error = shallowRef('')
watch(() => props.config, config => { Object.assign(form, config); recipients.value = config.recipients.join('\n') }, { immediate: true })
function submit() {
  const list = [...new Set(recipients.value.split(/[\n,，;]/).map(v => v.trim()).filter(Boolean))]
  if ((form.notify && !list.length) || list.length > 10 || list.some(v => !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v))) {
    error.value = '请填写有效的提醒邮箱，最多 10 个。'; return
  }
  error.value = ''
  emit('save', { ...form, recipients: list })
}
</script>
<template>
  <form class="space-y-5" @submit.prevent="submit">
    <div class="flex items-center justify-between"><label for="model-monitor-enabled" class="input-label mb-0">定时检测</label><Toggle id="model-monitor-enabled" v-model="form.enabled" /></div>
    <div><label for="model-monitor-interval" class="input-label">检测间隔（分钟）</label><input id="model-monitor-interval" v-model.number="form.interval_minutes" class="input" type="number" min="5" max="1440" step="1" required /></div>
    <div class="flex items-center justify-between"><label for="model-monitor-notify" class="input-label mb-0">模型变更邮件提醒</label><Toggle id="model-monitor-notify" v-model="form.notify" /></div>
    <div v-if="form.notify"><label for="model-monitor-recipients" class="input-label">收件邮箱</label><textarea id="model-monitor-recipients" v-model="recipients" class="input" rows="3" placeholder="一行一个邮箱" /></div>
    <p class="text-xs leading-5 text-gray-500 dark:text-dark-400">首次检测建立基线。后续变更需手动应用；同一批变更不会重复提醒。</p>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <div class="flex justify-end"><button type="submit" class="btn btn-primary" :disabled="busy">{{ busy ? '保存中…' : '保存设置' }}</button></div>
  </form>
</template>
