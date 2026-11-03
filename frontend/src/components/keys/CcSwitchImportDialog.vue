<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ApiKey } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { useCcSwitchImport } from '@/composables/useCcSwitchImport'
import { CC_SWITCH_FORMATS, CC_SWITCH_VERSION } from '@/utils/ccswitchImport'
import { maskApiKey } from '@/utils/maskApiKey'

const props = defineProps<{ apiKey: ApiKey | null; baseUrl: string; siteName: string }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()
const {
  group, clients, client, formats, format, providerName, model, models, modelsState,
  haikuModel, sonnetModel, opusModel, usageEnabled, usageAvailable, disableTelemetry,
  automaticModel, isClaude, needsRouting, endpoint, importResult, opened, refreshModels
} = useCcSwitchImport({ apiKey: () => props.apiKey, baseUrl: () => props.baseUrl, siteName: () => props.siteName })
const formatOptions = computed(() => CC_SWITCH_FORMATS.filter(item => formats.value.includes(item.value)))
const modelOptions = computed(() => models.value.map(value => ({ value, label: value })))
const primaryModelOptions = computed(() => automaticModel.value
  ? [{ value: '', label: t('keys.ccsImport.automaticModel') }, ...modelOptions.value] : modelOptions.value)
const aliasOptions = computed(() => [{ value: '', label: t('keys.ccsImport.aliasDefault') }, ...modelOptions.value])
const formatLabel = computed(() => CC_SWITCH_FORMATS.find(item => item.value === format.value)?.label || '')
const clientLabel = computed(() => clients.value.find(item => item.value === client.value)?.label || '')
function openImport() {
  if (!importResult.value.url) return
  try {
    // Keep dispatch inside the user gesture. Browser focus is not proof of installation or success.
    window.open(importResult.value.url, '_self')
    opened.value = true
  } catch { appStore.showError(t('keys.ccsImport.openFailed')) }
}
function copyImport() {
  if (importResult.value.url) void copyToClipboard(importResult.value.url)
}
</script>

<template>
  <BaseDialog :show="Boolean(apiKey)" :title="t('keys.importToCcSwitch')" width="normal" @close="emit('close')">
    <div class="space-y-5">
      <div class="flex flex-wrap items-center justify-between gap-2 text-xs text-gray-500 dark:text-dark-400">
        <span>{{ group?.name || t('keys.noGroup') }} · {{ maskApiKey(apiKey?.key || '') }}</span>
        <a href="https://github.com/farion1231/cc-switch/releases/latest" target="_blank" rel="noopener noreferrer" class="text-primary-600 hover:underline dark:text-primary-400">CC Switch {{ CC_SWITCH_VERSION }}+</a>
      </div>
      <p v-if="!clients.length" class="text-sm text-amber-700 dark:text-amber-300" role="status">{{ t('keys.ccsImport.unavailable') }}</p>
      <template v-else>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label class="input-label" for="ccs-client">{{ t('keys.ccsImport.client') }}</label>
            <Select id="ccs-client" v-model="client" :options="clients" :aria-label="t('keys.ccsImport.client')" />
          </div>
          <div>
            <label class="input-label" for="ccs-format">{{ t('keys.ccsImport.format') }}</label>
            <Select id="ccs-format" v-model="format" :options="formatOptions" :aria-label="t('keys.ccsImport.format')" :disabled="formatOptions.length === 1" />
          </div>
        </div>
        <div v-if="client === 'claude-desktop'" class="rounded-xl border border-primary-200 bg-primary-50 p-3 text-sm text-primary-800 dark:border-primary-900 dark:bg-primary-950/30 dark:text-primary-200" data-test="desktop-help">
          <p class="font-medium">{{ t('keys.ccsImport.desktopTitle') }}</p>
          <ol class="mt-2 list-inside list-decimal space-y-1">
            <li>{{ t('keys.ccsImport.desktopStep1') }}</li>
            <li>{{ t('keys.ccsImport.desktopStep2') }}</li>
            <li>{{ t('keys.ccsImport.desktopStep3') }}</li>
          </ol>
          <p class="mt-2 text-xs">{{ t('keys.ccsImport.desktopExisting') }}</p>
        </div>
        <p v-if="needsRouting" class="text-xs text-amber-700 dark:text-amber-300">{{ t('keys.ccsImport.routing') }}</p>
        <p v-if="group?.claude_code_only" class="text-xs text-gray-500 dark:text-dark-400">{{ t('keys.ccsImport.restricted') }}</p>
        <div>
          <label class="input-label" for="ccs-name">{{ t('keys.ccsImport.name') }}</label>
          <input id="ccs-name" v-model="providerName" class="input" maxlength="120" autocomplete="off" />
        </div>
        <div>
          <div class="mb-1.5 flex items-center justify-between gap-2">
            <label class="text-sm font-medium text-gray-700 dark:text-dark-200" for="ccs-model">{{ t('keys.ccsImport.model') }}</label>
            <button type="button" class="text-xs text-primary-600 hover:underline disabled:opacity-50 dark:text-primary-400" :disabled="modelsState === 'loading'" @click="refreshModels">
              {{ modelsState === 'loading' ? t('common.loading') : t('keys.ccsImport.refreshModels') }}
            </button>
          </div>
          <Select id="ccs-model" v-model="model" :options="primaryModelOptions" :creatable-prefix="t('keys.ccsImport.useModelId')" searchable creatable :loading="modelsState === 'loading'" :placeholder="t('keys.ccsImport.modelPlaceholder')" :aria-label="t('keys.ccsImport.model')" />
          <p class="mt-1.5 text-xs text-gray-500 dark:text-dark-400" aria-live="polite">
            {{ modelsState === 'error' ? t('keys.ccsImport.modelsFailed') : modelsState === 'ready' ? t('keys.ccsImport.modelsLoaded', { count: models.length }) : t('keys.ccsImport.modelsHint') }}
          </p>
        </div>
        <details v-if="isClaude" class="rounded-xl border border-gray-200 dark:border-dark-700">
          <summary class="cursor-pointer px-3 py-2.5 text-sm text-gray-700 dark:text-dark-200">{{ t('keys.ccsImport.aliases') }}</summary>
          <div class="space-y-3 px-3 pb-3">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('keys.ccsImport.aliasHint') }}</p>
            <div><label class="input-label" for="ccs-haiku">Haiku</label><Select id="ccs-haiku" v-model="haikuModel" :options="aliasOptions" :creatable-prefix="t('keys.ccsImport.useModelId')" searchable creatable aria-label="Haiku" /></div>
            <div><label class="input-label" for="ccs-sonnet">Sonnet</label><Select id="ccs-sonnet" v-model="sonnetModel" :options="aliasOptions" :creatable-prefix="t('keys.ccsImport.useModelId')" searchable creatable aria-label="Sonnet" /></div>
            <div><label class="input-label" for="ccs-opus">Opus</label><Select id="ccs-opus" v-model="opusModel" :options="aliasOptions" :creatable-prefix="t('keys.ccsImport.useModelId')" searchable creatable aria-label="Opus" /></div>
          </div>
        </details>
        <div class="space-y-2 text-sm text-gray-700 dark:text-dark-200">
          <label class="flex items-center gap-2"><input v-model="usageEnabled" type="checkbox" class="checkbox" :disabled="!usageAvailable" />{{ t('keys.ccsImport.balance') }}</label>
          <p v-if="!usageAvailable" class="text-xs text-amber-700 dark:text-amber-300">{{ t('keys.ccsImport.httpsUsage') }}</p>
          <label v-if="isClaude" class="flex items-center gap-2"><input v-model="disableTelemetry" type="checkbox" class="checkbox" />{{ t('keys.ccsImport.telemetry') }}</label>
        </div>
        <div class="rounded-xl bg-gray-50 p-3 text-xs dark:bg-dark-800">
          <p class="font-medium text-gray-700 dark:text-dark-200">{{ clientLabel }} · {{ formatLabel }}</p>
          <p class="mt-1 break-all font-mono text-gray-500 dark:text-dark-400" data-test="endpoint">{{ endpoint }}</p>
        </div>
        <p v-if="importResult.error" class="text-xs text-amber-700 dark:text-amber-300" role="status">{{ t(`keys.ccsImport.errors.${importResult.error}`) }}</p>
        <p v-if="opened" class="text-sm text-gray-600 dark:text-dark-300" role="status" data-test="opened">{{ t('keys.ccsImport.opened') }}</p>
        <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('keys.ccsImport.linkHint') }}</p>
      </template>
    </div>
    <template #footer>
      <div class="flex w-full flex-wrap justify-end gap-2">
        <button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.close') }}</button>
        <button v-if="clients.length" type="button" class="btn btn-secondary" :disabled="!importResult.url" data-test="copy" @click="copyImport">{{ t('keys.ccsImport.copyLink') }}</button>
        <button v-if="clients.length" type="button" class="btn btn-primary" :disabled="!importResult.url" data-test="import" @click="openImport">
          <Icon name="externalLink" size="sm" />{{ client === 'claude-desktop' ? t('keys.ccsImport.importDesktop') : t('keys.ccsImport.import') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>
