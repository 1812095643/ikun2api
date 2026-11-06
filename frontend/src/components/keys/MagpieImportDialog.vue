<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ApiKey } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { useMagpieImport } from '@/composables/useMagpieImport'
import { CC_SWITCH_FORMATS } from '@/utils/ccswitchImport'
import { maskApiKey } from '@/utils/maskApiKey'

const props = defineProps<{ apiKey: ApiKey | null; baseUrl: string; siteName: string }>()
const emit = defineEmits<{ close: []; back: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()
const { group, formats, format, providerName, model, models, modelsState, opened, endpoint, importResult, refreshModels } = useMagpieImport({
  apiKey: () => props.apiKey, baseUrl: () => props.baseUrl, siteName: () => props.siteName
})
const formatOptions = computed(() => CC_SWITCH_FORMATS.filter(item => formats.value.includes(item.value)))
const modelOptions = computed(() => [{ value: '', label: t('keys.magpieImport.automaticModel') }, ...models.value.map(value => ({ value, label: value }))])
function openImport() {
  if (!importResult.value.url) return
  try { window.open(importResult.value.url, '_self'); opened.value = true }
  catch { appStore.showError(t('keys.magpieImport.openFailed')) }
}
function copyImport() {
  if (importResult.value.url) void copyToClipboard(importResult.value.url)
}
</script>

<template>
  <BaseDialog :show="Boolean(apiKey)" :title="t('keys.magpieImport.title')" width="normal" @close="emit('close')">
    <div class="space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-2 text-xs text-gray-500 dark:text-dark-400">
        <span>{{ group?.name || t('keys.noGroup') }} · {{ maskApiKey(apiKey?.key || '') }}</span>
        <a href="https://usemagpie.ai" target="_blank" rel="noopener noreferrer" class="text-primary-600 hover:underline dark:text-primary-400">{{ t('keys.magpieImport.install') }}</a>
      </div>
      <template v-if="formats.length">
        <div>
          <label class="input-label" for="magpie-name">{{ t('keys.ccsImport.name') }}</label>
          <input id="magpie-name" v-model="providerName" class="input" maxlength="80" autocomplete="off" />
        </div>
        <div>
          <label class="input-label" for="magpie-format">{{ t('keys.ccsImport.format') }}</label>
          <Select id="magpie-format" v-model="format" :options="formatOptions" :disabled="formatOptions.length === 1" :aria-label="t('keys.ccsImport.format')" />
        </div>
        <div>
          <div class="mb-1.5 flex items-center justify-between gap-2">
            <label class="text-sm font-medium text-gray-700 dark:text-dark-200" for="magpie-model">{{ t('keys.magpieImport.model') }}</label>
            <button type="button" class="text-xs text-primary-600 hover:underline disabled:opacity-50 dark:text-primary-400" :disabled="modelsState === 'loading'" data-test="refresh-models" @click="refreshModels">{{ modelsState === 'loading' ? t('common.loading') : t('keys.ccsImport.refreshModels') }}</button>
          </div>
          <Select id="magpie-model" v-model="model" :options="modelOptions" :creatable-prefix="t('keys.ccsImport.useModelId')" searchable creatable :loading="modelsState === 'loading'" :aria-label="t('keys.magpieImport.model')" />
          <p v-if="modelsState === 'error'" class="mt-1.5 text-xs text-amber-700 dark:text-amber-300" role="status">{{ t('keys.magpieImport.modelsFailed') }}</p>
        </div>
        <p v-if="endpoint" class="break-all rounded-xl bg-gray-50 p-3 font-mono text-xs text-gray-500 dark:bg-dark-800 dark:text-dark-400" data-test="endpoint">{{ endpoint }}</p>
      </template>
      <p v-if="importResult.error" class="text-xs text-amber-700 dark:text-amber-300" role="status">{{ t(importResult.error === 'httpsRequired' ? 'keys.magpieImport.httpsRequired' : importResult.error === 'invalidName' ? 'keys.magpieImport.invalidName' : `keys.ccsImport.errors.${importResult.error}`) }}</p>
      <p v-if="opened" class="text-sm text-gray-600 dark:text-dark-300" role="status" data-test="opened">{{ t('keys.magpieImport.opened') }}</p>
    </div>
    <template #footer>
      <div class="flex w-full flex-wrap justify-end gap-2">
        <button type="button" class="btn btn-secondary mr-auto" data-test="back" @click="emit('back')">{{ t('common.back') }}</button>
        <button type="button" class="btn btn-secondary" :disabled="!importResult.url" :title="t('keys.ccsImport.linkHint')" data-test="copy" @click="copyImport">{{ t('keys.ccsImport.copyLink') }}</button>
        <button type="button" class="btn btn-primary" :disabled="!importResult.url" data-test="import" @click="openImport"><Icon name="externalLink" size="sm" />{{ t('keys.magpieImport.import') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>
