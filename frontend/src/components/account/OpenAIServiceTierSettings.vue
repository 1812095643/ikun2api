<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { defaultUltrafastMultiplier, type OpenAIServiceTierDraft } from '@/utils/openaiServiceTiers'

const props = defineProps<{ modelValue: OpenAIServiceTierDraft }>()
const emit = defineEmits<{ 'update:modelValue': [value: OpenAIServiceTierDraft] }>()
const { t } = useI18n()
function update(patch: Partial<OpenAIServiceTierDraft>) { emit('update:modelValue', { ...props.modelValue, ...patch }) }
function updateRow(index: number, field: 'model' | 'fast' | 'ultrafast', value: string) {
  update({ models: props.modelValue.models.map((row, i) => i === index ? { ...row, [field]: value } : row) })
}
</script>

<template>
  <section class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-400" data-testid="openai-service-tier-settings">
    <h4 class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ t('openaiServiceTiers.title') }}</h4>
    <div class="grid grid-cols-2 gap-3">
      <label class="flex items-center gap-2 text-sm"><input type="checkbox" class="checkbox" :checked="modelValue.fastEnabled" @change="update({ fastEnabled: ($event.target as HTMLInputElement).checked })" />{{ t('openaiServiceTiers.supportFast') }}</label>
      <label class="flex items-center gap-2 text-sm"><input type="checkbox" class="checkbox" :checked="modelValue.ultrafastEnabled" @change="update({ ultrafastEnabled: ($event.target as HTMLInputElement).checked })" />{{ t('openaiServiceTiers.supportUltrafast') }}</label>
      <label class="text-xs text-gray-500 dark:text-gray-400">{{ t('openaiServiceTiers.fastMultiplier') }}<input type="number" min="0.0001" max="1000" step="0.0001" class="input mt-1" :value="modelValue.fast" :placeholder="t('openaiServiceTiers.modelDefault')" @input="update({ fast: ($event.target as HTMLInputElement).value })" /></label>
      <label class="text-xs text-gray-500 dark:text-gray-400">{{ t('openaiServiceTiers.ultrafastMultiplier') }}<input type="number" min="0.0001" max="1000" step="0.0001" class="input mt-1" :value="modelValue.ultrafast" :placeholder="t('openaiServiceTiers.modelDefault')" @input="update({ ultrafast: ($event.target as HTMLInputElement).value })" /></label>
    </div>
    <div class="flex items-center justify-between gap-2">
      <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('openaiServiceTiers.modelOverrides') }}</span>
      <button type="button" class="text-xs font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400" @click="update({ models: [...modelValue.models, { model: '', fast: '', ultrafast: '' }] })">{{ t('common.add') }}</button>
    </div>
    <div class="space-y-2">
      <div class="grid grid-cols-[minmax(0,1.7fr)_minmax(0,1fr)_minmax(0,1fr)_1.5rem] gap-2 text-[11px] text-gray-500 dark:text-gray-400"><span>{{ t('openaiServiceTiers.model') }}</span><span>Fast ×</span><span>Ultrafast ×</span><span /></div>
      <div v-for="(row, index) in modelValue.models" :key="index" class="grid grid-cols-[minmax(0,1.7fr)_minmax(0,1fr)_minmax(0,1fr)_1.5rem] items-center gap-2">
        <input :value="row.model" class="input min-w-0 !px-2 text-xs" :aria-label="t('openaiServiceTiers.model')" placeholder="gpt-*" maxlength="200" @input="updateRow(index, 'model', ($event.target as HTMLInputElement).value)" />
        <input :value="row.fast" type="number" min="0.0001" max="1000" step="0.0001" class="input min-w-0 !px-2 text-xs" :aria-label="`${row.model} Fast`" :placeholder="modelValue.fast || t('openaiServiceTiers.modelDefault')" @input="updateRow(index, 'fast', ($event.target as HTMLInputElement).value)" />
        <input :value="row.ultrafast" type="number" min="0.0001" max="1000" step="0.0001" class="input min-w-0 !px-2 text-xs" :aria-label="`${row.model} Ultrafast`" :placeholder="modelValue.ultrafast || String(defaultUltrafastMultiplier(row.model) ?? t('openaiServiceTiers.modelDefault'))" @input="updateRow(index, 'ultrafast', ($event.target as HTMLInputElement).value)" />
        <button type="button" class="rounded p-1 text-gray-400 hover:text-red-500 focus-visible:ring-2 focus-visible:ring-primary-500" :aria-label="t('common.delete')" @click="update({ models: modelValue.models.filter((_, i) => i !== index) })"><svg viewBox="0 0 20 20" fill="none" stroke="currentColor" class="h-4 w-4" aria-hidden="true"><path d="m6 6 8 8M6 14l8-8" stroke-width="1.5" stroke-linecap="round" /></svg></button>
      </div>
    </div>
  </section>
</template>
