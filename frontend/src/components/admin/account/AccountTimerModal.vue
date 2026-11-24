<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { schedulableTimersAPI, type TimerInput, type TimerAction, type TimerSnapshot, type TimerPreview, type SchedulableTimer } from '@/api/admin/schedulableTimers'
import { useAppStore } from '@/stores/app'
import { extractApiErrorCode } from '@/utils/apiError'

const props = defineProps<{ show: boolean; accountId: number | null; accountName?: string }>()
const emit = defineEmits<{ close: []; updated: [] }>()
const { t, te } = useI18n()
const app = useAppStore()
const loading = ref(false), busy = ref(false), error = ref(''), tab = ref<'plan' | 'history'>('plan')
const snapshot = ref<TimerSnapshot | null>(null), preview = ref<TimerPreview | null>(null)
const action = ref<TimerAction>('open')
const localDay = () => new Date(Date.now() - new Date().getTimezoneOffset() * 60000).toISOString().slice(0, 10)
const form = ref<TimerInput>({ mode: 'relative', hours: 1, minutes: 0, date: localDay(), time: '08:00', timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC', weekdays_only: false, periods: [{ start: '08:00', end: '20:00' }] })
const modes = computed(() => ['relative', 'absolute', 'recurring'].map(value => ({ value, label: t(`accountTimer.${value}`) })))
const actions = computed(() => ['open', 'close'].map(value => ({ value, label: t(`accountTimer.${value}`) })))
const pending = computed(() => snapshot.value?.tasks.filter(task => task.status === 'pending') ?? [])
let epoch = 0
const reason = (value: string) => te(`accountTimer.reasons.${value}`) ? t(`accountTimer.reasons.${value}`) : t('accountTimer.invalid')
function failure(err: unknown) {
  const code = extractApiErrorCode(err)?.replace('TIMER_', '')
  const key = code === 'timezone' ? 'invalidTimezone' : code
  return key && te(`accountTimer.${key}`) ? t(`accountTimer.${key}`) : t('accountTimer.saveError')
}
async function load() {
  if (!props.accountId) return
  const current = ++epoch
  loading.value = true; error.value = ''
  try { const data = await schedulableTimersAPI.list(props.accountId); if (current === epoch) snapshot.value = data }
  catch { if (current === epoch) error.value = t('accountTimer.loadError') }
  finally { if (current === epoch) loading.value = false }
}
function input(): TimerInput {
  const value = { ...form.value }
  if (value.mode === 'relative' || value.mode === 'recurring') { value.date = ''; value.time = '' }
  if (value.mode !== 'relative') { value.hours = 0; value.minutes = 0 }
  if (value.mode !== 'recurring') { value.weekdays_only = false; value.periods = [] }
  return value
}
function edit(task: SchedulableTimer) {
  action.value = task.action
  form.value = { ...form.value, mode: 'absolute', hours: 0, minutes: 0, date: task.local_date || '', time: task.local_time || '', timezone: task.timezone }
}
function editRecurring() {
  const value = snapshot.value?.recurring
  if (!value) return
  form.value = { ...form.value, mode: 'recurring', weekdays_only: value.weekdays_only, timezone: value.timezone, periods: value.periods.map(period => ({ ...period })) }
}
async function showPreview() {
  if (!props.accountId) return
  busy.value = true; error.value = ''
  try { preview.value = await schedulableTimersAPI.preview(props.accountId, input()) }
  catch (err) { error.value = failure(err) }
  finally { busy.value = false }
}
async function save() {
  if (!props.accountId || !snapshot.value) return
  if (!preview.value) { await showPreview(); return }
  busy.value = true; error.value = ''
  const selectedAction = form.value.mode === 'recurring' ? 'recurring' : action.value
  const version = selectedAction === 'recurring' ? snapshot.value.recurring?.version ?? 0 : snapshot.value.tasks.find(task => task.action === selectedAction)?.version ?? 0
  try {
    await schedulableTimersAPI.save(props.accountId, { ...input(), action: selectedAction, expected_version: version, confirmed_at: preview.value.execute_at })
    app.showSuccess(t('accountTimer.saved')); preview.value = null; await load(); emit('updated')
  } catch (err) { error.value = failure(err); preview.value = null }
  finally { busy.value = false }
}
async function cancel(selectedAction: TimerAction, version: number) {
  if (!props.accountId) return
  busy.value = true; error.value = ''
  try { await schedulableTimersAPI.cancel(props.accountId, selectedAction, version); app.showSuccess(t('accountTimer.cancelled')); await load(); emit('updated') }
  catch (err) { error.value = failure(err) }
  finally { busy.value = false }
}
watch([form, action], () => { preview.value = null }, { deep: true })
watch(() => [props.show, props.accountId] as const, ([show]) => { snapshot.value = null; preview.value = null; error.value = ''; tab.value = 'plan'; if (show) void load(); else epoch++ }, { immediate: true })
</script>

<template>
  <BaseDialog :show="show" :title="`${t('accountTimer.title')} · ${accountName || accountId}`" width="normal" :close-on-escape="!busy" :show-close-button="!busy" @close="emit('close')">
    <div class="space-y-4">
      <div class="flex gap-1 border-b border-gray-200 dark:border-dark-700">
        <button v-for="item in ['plan', 'history'] as const" :key="item" type="button" class="border-b-2 px-3 py-2 text-sm font-medium" :class="tab === item ? 'border-primary-500 text-primary-600 dark:text-primary-400' : 'border-transparent text-gray-500'" @click="tab = item">{{ t(`accountTimer.${item}`) }}</button>
      </div>
      <p v-if="loading" class="py-4 text-sm text-gray-500">{{ t('accountTimer.loading') }}</p>
      <p v-if="error" role="alert" class="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ error }}</p>
      <template v-if="snapshot && tab === 'plan'">
        <div class="space-y-2">
          <div v-if="snapshot.recurring?.enabled" class="flex items-center justify-between gap-3 rounded-lg bg-primary-50 px-3 py-2 dark:bg-primary-900/15">
            <div class="min-w-0 text-sm"><span class="font-medium">{{ t('accountTimer.recurring') }}</span><span class="ml-2 text-gray-500">{{ snapshot.recurring.periods.map(period => `${period.start}–${period.end}`).join(' · ') }} · {{ snapshot.recurring.timezone }}</span></div>
            <button type="button" class="shrink-0 text-xs text-primary-600" :disabled="busy" @click="editRecurring">{{ t('common.edit') }}</button>
            <button type="button" class="shrink-0 text-xs text-red-600" :disabled="busy" @click="cancel('recurring', snapshot.recurring.version)">{{ t('accountTimer.cancel') }}</button>
          </div>
          <div v-for="task in pending" :key="task.action" class="flex items-center justify-between gap-3 rounded-lg bg-gray-50 px-3 py-2 dark:bg-dark-800">
            <div class="text-sm"><span class="font-medium">{{ t(`accountTimer.${task.action}`) }}</span><span class="ml-2 text-gray-500">{{ task.local_date }} {{ task.local_time }} · {{ task.timezone }}</span></div>
            <button type="button" class="shrink-0 text-xs text-primary-600" :disabled="busy" @click="edit(task)">{{ t('common.edit') }}</button>
            <button type="button" class="shrink-0 text-xs text-red-600" :disabled="busy" @click="cancel(task.action, task.version)">{{ t('accountTimer.cancel') }}</button>
          </div>
          <p v-if="!pending.length && !snapshot.recurring?.enabled" class="py-1 text-sm text-gray-500">{{ t('accountTimer.noPlan') }}</p>
        </div>
        <fieldset :disabled="busy" class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-700">
          <div class="grid grid-cols-2 gap-3"><Select v-model="form.mode" :options="modes" :aria-label="t('accountTimer.plan')" /><Select v-if="form.mode !== 'recurring'" v-model="action" :options="actions" :aria-label="t('accountTimer.action')" /><label v-else class="flex items-center gap-2 text-sm"><input v-model="form.weekdays_only" type="checkbox" class="rounded" />{{ t('accountTimer.weekdays') }}</label></div>
          <div v-if="form.mode === 'relative'" class="grid grid-cols-2 gap-3"><label class="space-y-1 text-xs text-gray-500">{{ t('accountTimer.hours') }}<input v-model.number="form.hours" type="number" min="0" max="8784" class="input w-full" /></label><label class="space-y-1 text-xs text-gray-500">{{ t('accountTimer.minutes') }}<input v-model.number="form.minutes" type="number" min="0" max="59" class="input w-full" /></label></div>
          <div v-else-if="form.mode === 'absolute'" class="grid grid-cols-2 gap-3"><label class="space-y-1 text-xs text-gray-500">{{ t('accountTimer.date') }}<input v-model="form.date" type="date" class="input w-full" /></label><label class="space-y-1 text-xs text-gray-500">{{ t('accountTimer.time') }}<input v-model="form.time" type="time" class="input w-full" /></label></div>
          <div v-else class="space-y-2">
            <div v-for="(period, index) in form.periods" :key="index" class="flex items-center gap-2"><input v-model="period.start" type="time" class="input min-w-0 flex-1" :aria-label="t('connections.start')" /><span class="text-gray-400">–</span><input v-model="period.end" type="time" class="input min-w-0 flex-1" :aria-label="t('connections.end')" /><button v-if="(form.periods?.length ?? 0) > 1" type="button" class="p-2 text-gray-400 hover:text-red-500" :aria-label="t('common.delete')" @click="form.periods?.splice(index, 1)"><Icon name="x" size="sm" /></button></div>
            <button v-if="(form.periods?.length ?? 0) < 8" type="button" class="text-xs font-medium text-primary-600" @click="form.periods?.push({ start: '08:00', end: '20:00' })">{{ t('accountTimer.addPeriod') }}</button>
          </div>
          <label class="block space-y-1 text-xs text-gray-500">{{ t('accountTimer.timezone') }}<input v-model.trim="form.timezone" class="input w-full" list="account-timer-timezones" /></label>
          <datalist id="account-timer-timezones"><option v-for="zone in ['Asia/Shanghai', 'Asia/Tokyo', 'America/New_York', 'America/Los_Angeles', 'Europe/London', 'UTC']" :key="zone" :value="zone" /></datalist>
          <div v-if="preview" class="rounded-lg bg-primary-50 px-3 py-2 text-sm font-medium tabular-nums text-primary-700 dark:bg-primary-900/20 dark:text-primary-300">{{ t('accountTimer.previewAt', { date: preview.local_date, time: preview.local_time, zone: preview.timezone }) }}</div>
        </fieldset>
      </template>
      <div v-else-if="snapshot" class="max-h-96 space-y-3 overflow-y-auto">
        <p v-if="!snapshot.history.length" class="text-sm text-gray-500">{{ t('accountTimer.noHistory') }}</p>
        <div v-for="(item, index) in snapshot.history" :key="index" class="flex items-start justify-between gap-3 border-b border-gray-100 pb-2 text-xs dark:border-dark-800">
          <div><p class="font-medium">{{ t(`accountTimer.${item.action.includes('close') ? 'close' : item.action.includes('open') ? 'open' : 'recurring'}`) }} · {{ t(`accountTimer.statuses.${item.result}`) }}</p><p v-if="item.reason" class="mt-1 text-gray-500">{{ reason(item.reason) }}</p></div><time class="shrink-0 tabular-nums text-gray-500">{{ new Date(item.actual_at).toLocaleString() }}</time>
        </div>
      </div>
      <button v-if="!snapshot && !loading" type="button" class="btn btn-secondary" @click="load">{{ t('common.tryAgain') }}</button>
    </div>
    <template #footer><button type="button" class="btn btn-secondary" :disabled="busy" @click="emit('close')">{{ t('common.close') }}</button><button v-if="tab === 'plan'" type="button" class="btn btn-primary" :disabled="busy || loading || !snapshot" @click="save">{{ preview ? t('accountTimer.save') : t('accountTimer.preview') }}</button></template>
  </BaseDialog>
</template>
