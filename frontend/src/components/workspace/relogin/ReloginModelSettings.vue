<script setup lang="ts">
import { shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import ModelWhitelistSelector from '@/components/account/ModelWhitelistSelector.vue'
import Icon from '@/components/icons/Icon.vue'
import { getPresetMappingsByPlatform, type ModelMappingEntry } from '@/composables/useModelWhitelist'
import { createStableObjectKeyResolver } from '@/utils/stableObjectKey'

const props = defineProps<{ disabled: boolean }>()
const allowedModels = defineModel<string[]>('allowedModels', { required: true })
const modelMappings = defineModel<ModelMappingEntry[]>('modelMappings', { required: true })
const { t } = useI18n()
const mode = shallowRef<'whitelist' | 'mapping'>(
  modelMappings.value.length > 0 && allowedModels.value.length === 0 ? 'mapping' : 'whitelist'
)
const presets = getPresetMappingsByPlatform('openai')
const mappingKey = createStableObjectKeyResolver<ModelMappingEntry>('relogin-model-mapping')

function addMapping(from = '', to = '') {
  if (props.disabled || (from && modelMappings.value.some(mapping => mapping.from.trim() === from))) return
  modelMappings.value = [...modelMappings.value, { from, to }]
}

function updateMapping(index: number, field: 'from' | 'to', event: Event) {
  const value = (event.target as HTMLInputElement).value
  modelMappings.value[index][field] = value
  modelMappings.value = [...modelMappings.value]
}

function removeMapping(index: number) {
  modelMappings.value = modelMappings.value.filter((_, row) => row !== index)
}
</script>

<template>
  <div>
    <label class="input-label">{{ t('admin.accounts.modelRestriction') }}</label>
    <div class="mb-4 flex gap-2" role="group" aria-label="模型限制方式">
      <button
        type="button"
        :aria-pressed="mode === 'whitelist'"
        :disabled="disabled"
        :class="[
          'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-colors',
          mode === 'whitelist'
            ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
            : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
        ]"
        @click="mode = 'whitelist'"
      >{{ t('admin.accounts.modelWhitelist') }}</button>
      <button
        type="button"
        :aria-pressed="mode === 'mapping'"
        :disabled="disabled"
        :class="[
          'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-colors',
          mode === 'mapping'
            ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
            : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
        ]"
        @click="mode = 'mapping'"
      >{{ t('admin.accounts.modelMapping') }}</button>
    </div>
    <ModelWhitelistSelector
      v-if="mode === 'whitelist'"
      v-model="allowedModels"
      :model-mappings="modelMappings"
      platform="openai"
    />
    <p v-if="mode === 'whitelist' && allowedModels.length === 0 && modelMappings.length === 0" class="input-hint">
      {{ t('admin.accounts.supportsAllModels') }}
    </p>
    <div v-if="mode === 'mapping'">
      <div class="mb-3 space-y-2">
        <div v-for="(mapping, index) in modelMappings" :key="mappingKey(mapping)" class="flex min-w-0 items-center gap-2">
          <input
            :value="mapping.from"
            class="input min-w-0 flex-1"
            type="text"
            :aria-label="`来源模型 ${index + 1}`"
            :placeholder="t('admin.accounts.requestModel')"
            @input="updateMapping(index, 'from', $event)"
          />
          <Icon name="arrowRight" size="sm" class="shrink-0 text-gray-400" aria-hidden="true" />
          <input
            :value="mapping.to"
            class="input min-w-0 flex-1"
            type="text"
            :aria-label="`上游模型 ${index + 1}`"
            :placeholder="t('admin.accounts.actualModel')"
            @input="updateMapping(index, 'to', $event)"
          />
          <button
            type="button"
            class="shrink-0 rounded-lg p-2 text-red-500 transition-colors hover:bg-red-50 dark:hover:bg-red-900/20"
            :aria-label="`删除映射 ${index + 1}`"
            @click="removeMapping(index)"
          ><Icon name="trash" size="sm" /></button>
        </div>
      </div>
      <button
        type="button"
        class="mb-3 w-full rounded-lg border-2 border-dashed border-gray-300 px-4 py-2 text-sm text-gray-600 transition-colors hover:border-gray-400 dark:border-dark-500 dark:text-gray-400 dark:hover:border-dark-400"
        @click="addMapping()"
      >+ {{ t('admin.accounts.addMapping') }}</button>
      <div class="flex flex-wrap gap-2">
        <button
          v-for="preset in presets"
          :key="preset.label"
          type="button"
          :class="['rounded-lg px-3 py-1 text-xs transition-colors', preset.color]"
          @click="addMapping(preset.from, preset.to)"
        >+ {{ preset.label }}</button>
      </div>
    </div>
  </div>
</template>
