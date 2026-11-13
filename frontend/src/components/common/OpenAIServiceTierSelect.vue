<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import type { ForcedOpenAIServiceTier } from '@/utils/openaiServiceTiers'

defineProps<{ modelValue: ForcedOpenAIServiceTier }>()
const emit = defineEmits<{ 'update:modelValue': [value: ForcedOpenAIServiceTier] }>()
const { t } = useI18n()
const options = computed(() => [
  { value: '', label: t('openaiServiceTiers.followRequest') },
  { value: 'priority', label: t('openaiServiceTiers.forceFast') },
  { value: 'ultrafast', label: t('openaiServiceTiers.forceUltrafast') },
])
function update(value: string | number | boolean | null) {
  if (value === '' || value === 'priority' || value === 'ultrafast') emit('update:modelValue', value)
}
</script>

<template>
  <Select :model-value="modelValue" :options="options" :searchable="false" :aria-label="t('openaiServiceTiers.forcedTier')" @update:model-value="update" />
</template>
