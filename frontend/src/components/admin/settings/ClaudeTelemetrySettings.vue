<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import { getClaudeTelemetrySettings, updateClaudeTelemetrySettings } from '@/api/admin/claudeTelemetry'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const form = reactive({ enabled: true, flush_interval_seconds: 10, min_send_gap_seconds: 2, timeout_seconds: 10 })
const loading = ref(true)
const loaded = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const fields = computed(() => [
  { key: 'flush_interval_seconds' as const, label: t('admin.settings.telemetry.flushInterval'), min: 5, max: 300 },
  { key: 'min_send_gap_seconds' as const, label: t('admin.settings.telemetry.minSendGap'), min: 2, max: 60 },
  { key: 'timeout_seconds' as const, label: t('admin.settings.telemetry.timeout'), min: 3, max: 60 },
])

async function load() {
  loading.value = true
  loaded.value = false
  error.value = ''
  try {
    Object.assign(form, await getClaudeTelemetrySettings())
    loaded.value = true
  } catch (cause) {
    error.value = extractApiErrorMessage(cause, t('admin.settings.telemetry.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!loaded.value || saving.value) return
  error.value = ''
  notice.value = ''
  if (fields.value.some(field => !Number.isInteger(form[field.key]) || form[field.key] < field.min || form[field.key] > field.max)) {
    error.value = t('admin.settings.telemetry.invalid')
    return
  }
  saving.value = true
  try {
    Object.assign(form, await updateClaudeTelemetrySettings({ ...form }))
    notice.value = t('admin.settings.telemetry.saved')
  } catch (cause) {
    error.value = extractApiErrorMessage(cause, t('admin.settings.telemetry.saveFailed'))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <section class="card" aria-labelledby="claude-telemetry-title" data-testid="claude-telemetry-settings">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 id="claude-telemetry-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.telemetry.title') }}</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.telemetry.description') }}</p>
    </div>
    <div class="space-y-5 p-6">
      <p v-if="loading" role="status" class="text-sm text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</p>
      <fieldset :disabled="!loaded || saving" class="space-y-5">
        <div class="flex items-center justify-between gap-4">
          <div>
            <label id="claude-telemetry-enabled" class="font-medium text-gray-900 dark:text-white">{{ t('admin.settings.telemetry.enabled') }}</label>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.telemetry.privacyHint') }}</p>
          </div>
          <Toggle v-model="form.enabled" aria-labelledby="claude-telemetry-enabled" />
        </div>
        <div class="grid gap-4 sm:grid-cols-3">
          <label v-for="field in fields" :key="field.key" class="block text-sm font-medium text-gray-700 dark:text-gray-300">
            {{ field.label }}
            <input v-model.number="form[field.key]" :data-testid="field.key" type="number" :min="field.min" :max="field.max" step="1" class="input mt-2 w-full" />
            <span class="mt-1 block text-xs font-normal text-gray-500 dark:text-gray-400">{{ field.min }}–{{ field.max }}</span>
          </label>
        </div>
      </fieldset>
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <p v-if="notice" role="status" class="text-sm text-gray-600 dark:text-gray-300">{{ notice }}</p>
      <div class="flex justify-end gap-3 border-t border-gray-100 pt-4 dark:border-dark-700">
        <button v-if="!loaded && !loading" type="button" class="btn btn-secondary btn-sm" @click="load">{{ t('admin.settings.telemetry.retry') }}</button>
        <button type="button" class="btn btn-primary btn-sm" :disabled="!loaded || loading || saving" data-testid="save-telemetry" @click="save">{{ saving ? t('common.saving') : t('admin.settings.telemetry.save') }}</button>
      </div>
    </div>
  </section>
</template>
