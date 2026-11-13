<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import Select from '@/components/common/Select.vue'

const props = withDefaults(defineProps<{ modelValue: string; allowInherit?: boolean; bordered?: boolean; label?: string }>(), { allowInherit: true, bordered: true, label: '' })
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const { t } = useI18n()
const names = ref(['UTC', 'Asia/Singapore', 'Asia/Shanghai', 'Asia/Tokyo', 'America/New_York', 'America/Los_Angeles', 'Europe/London'])
const loading = ref(false)
const loadFailed = ref(false)
const options = computed(() => {
  const values = [...new Set([...names.value, ...(props.modelValue ? [props.modelValue] : [])])].sort()
  return [...(props.allowInherit ? [{ value: '', label: t('openaiServiceTiers.inheritTimezone') }] : []), ...values.map(value => ({ value, label: value }))]
})
function selectTimezone(value: string | number | boolean | null) {
  if (typeof value === 'string') emit('update:modelValue', value)
}
onMounted(async () => {
  loading.value = true
  try {
    const result = await adminAPI.accounts.getOpenAIRequestTimezones()
    if (!Array.isArray(result.timezones) || !result.timezones.length) throw new Error('Timezone catalog unavailable')
    names.value = result.timezones
  } catch { loadFailed.value = true }
  finally { loading.value = false }
})
</script>

<template>
  <div :class="bordered ? 'border-t border-gray-200 pt-4 dark:border-dark-600' : ''">
    <label class="input-label">{{ label || t('admin.accounts.openai.requestTimezone') }}</label>
    <Select :model-value="modelValue" :options="options" :loading="loading" searchable :aria-label="label || t('admin.accounts.openai.requestTimezone')" @update:model-value="selectTimezone" />
    <p v-if="loadFailed" class="mt-1 text-xs text-amber-600 dark:text-amber-400">{{ t('openaiServiceTiers.timezoneLoadFailed') }}</p>
  </div>
</template>
