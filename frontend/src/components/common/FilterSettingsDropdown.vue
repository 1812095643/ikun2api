<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

defineProps<{ options: { key: string; label: string }[]; hidden: string[] }>()
const emit = defineEmits<{ toggle: [key: string]; reset: [] }>()
const { t } = useI18n()
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const open = ref(false)
function outside(event: PointerEvent) {
  if (event.target instanceof Node && !root.value?.contains(event.target)) open.value = false
}
function escape() { open.value = false; trigger.value?.focus() }
onMounted(() => document.addEventListener('pointerdown', outside))
onUnmounted(() => document.removeEventListener('pointerdown', outside))
</script>

<template>
  <div ref="root" class="relative" @keydown.esc.stop.prevent="escape">
    <button ref="trigger" type="button" data-testid="usage-filter-settings" class="btn btn-secondary px-2 md:px-3" :title="t('usage.filterSettings')" :aria-label="t('usage.filterSettings')" :aria-expanded="open" aria-haspopup="menu" @click="open = !open">
      <svg class="h-4 w-4 md:mr-1.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M4 6h16M7 12h10M10 18h4" /></svg>
      <span class="hidden md:inline">{{ t('usage.filterSettings') }}</span>
    </button>
    <div v-if="open" role="menu" :aria-label="t('usage.filterSettings')" class="absolute right-0 top-full z-50 mt-1 max-h-80 w-56 overflow-y-auto rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-dark-600 dark:bg-dark-800">
      <button v-for="option in options" :key="option.key" type="button" role="menuitemcheckbox" :aria-checked="!hidden.includes(option.key)" :data-testid="`usage-filter-toggle-${option.key}`" class="flex w-full items-center justify-between gap-3 px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700" @click="emit('toggle', option.key)">
        <span>{{ option.label }}</span><Icon v-if="!hidden.includes(option.key)" name="check" size="sm" class="text-primary-500" />
      </button>
      <div class="mt-1 border-t border-gray-100 pt-1 dark:border-dark-700"><button type="button" role="menuitem" data-testid="usage-filter-reset" class="w-full px-4 py-2 text-left text-sm text-primary-600 hover:bg-gray-100 dark:text-primary-400 dark:hover:bg-dark-700" @click="emit('reset')">{{ t('usage.restoreFilters') }}</button></div>
    </div>
  </div>
</template>
