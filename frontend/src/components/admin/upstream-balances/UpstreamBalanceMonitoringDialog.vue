<script setup lang="ts">
import { reactive, ref } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import { upstreamBalancesAPI, type BalanceMonitoring } from '@/api/upstreamBalances'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ initial: BalanceMonitoring; selectedIds: string[]; total: number }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const app = useAppStore()
const form = reactive({ ...props.initial, monitor_enabled: true })
const target = ref(props.selectedIds.length ? 'selected' : 'all')
const recipients = ref(props.initial.recipients.join('\n'))
const saving = ref(false)
const error = ref('')
const targets = [
  { value: 'all', label: `全部上游（${props.total}）` },
  ...(props.selectedIds.length ? [{ value: 'selected', label: `选中上游（${props.selectedIds.length}）` }] : [])
]
async function save() {
  if (saving.value) return
  const list = [...new Set(recipients.value.split(/[\s,;，；]+/).map(value => value.trim()).filter(Boolean))]
  if (list.length > 10 || list.some(value => !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value))) {
    error.value = '请填写有效的提醒邮箱，最多 10 个。'
    return
  }
  saving.value = true
  error.value = ''
  try {
    const result = await upstreamBalancesAPI.saveMonitoring({ ...form, recipients: list }, target.value === 'selected' ? props.selectedIds : undefined)
    app.showSuccess(`已更新 ${result.affected} 个上游的余额告警`)
    emit('saved')
    emit('close')
  } catch (e) {
    error.value = extractApiErrorMessage(e, '暂时无法保存，请稍后重试。')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <BaseDialog :show="true" title="余额告警" width="normal" :show-close-button="!saving" :close-on-escape="!saving" @close="!saving && emit('close')">
    <form id="upstream-balance-monitoring" class="space-y-4" @submit.prevent="save">
      <fieldset :disabled="saving" class="space-y-4">
        <div class="flex items-center justify-between gap-4">
          <div class="flex-1"><label class="input-label">应用范围</label><Select v-model="target" :options="targets" :disabled="saving" aria-label="应用范围" /></div>
          <label class="flex shrink-0 items-center gap-3 pt-5 text-sm"><span>启用告警</span><Toggle v-model="form.monitor_enabled" :disabled="saving" /></label>
        </div>
        <div class="grid gap-4 sm:grid-cols-2">
          <div><label for="balance-bulk-interval" class="input-label">检查间隔（分钟）</label><input id="balance-bulk-interval" v-model.number="form.interval_minutes" class="input" type="number" min="5" max="1440" required /></div>
          <div><label for="balance-bulk-threshold" class="input-label">低余额阈值</label><input id="balance-bulk-threshold" v-model.number="form.threshold" class="input" type="number" min="0" max="1000000000000" step="any" required /></div>
        </div>
        <div><label for="balance-bulk-emails" class="input-label">提醒邮箱</label><textarea id="balance-bulk-emails" v-model="recipients" class="input" rows="3" placeholder="每行一个邮箱" /></div>
      </fieldset>
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
    </form>
    <template #footer>
      <button class="btn btn-secondary" :disabled="saving" @click="emit('close')">取消</button>
      <button type="submit" form="upstream-balance-monitoring" class="btn btn-primary" :disabled="saving">{{ saving ? '保存中…' : form.monitor_enabled ? '保存并启用' : '保存' }}</button>
    </template>
  </BaseDialog>
</template>
