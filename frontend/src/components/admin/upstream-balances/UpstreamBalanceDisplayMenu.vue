<script setup lang="ts">
import { onMounted, onUnmounted, shallowRef, useTemplateRef } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { balanceColumns, type BalanceColumn, type UpstreamBalanceDisplay } from '@/api/upstreamBalances'

defineProps<{ display: UpstreamBalanceDisplay; disabled?: boolean }>()
const emit = defineEmits<{ toggle: [key: BalanceColumn]; compact: []; reset: [] }>()
const root = useTemplateRef<HTMLElement>('root')
const trigger = useTemplateRef<HTMLButtonElement>('trigger')
const open = shallowRef(false)
function outside(event: PointerEvent) { if (event.target instanceof Node && !root.value?.contains(event.target)) open.value = false }
function close() { open.value = false; trigger.value?.focus() }
onMounted(() => document.addEventListener('pointerdown', outside))
onUnmounted(() => document.removeEventListener('pointerdown', outside))
</script>

<template>
  <div ref="root" class="relative" @keydown.esc.stop.prevent="close">
    <button ref="trigger" type="button" class="btn btn-secondary gap-2" aria-label="列表设置" aria-haspopup="menu" :aria-expanded="open" :disabled="disabled" @click="open = !open"><Icon name="grid" size="sm" /><span class="hidden sm:inline">列表设置</span></button>
    <div v-if="open" role="menu" aria-label="列表设置" class="absolute right-0 top-full z-50 mt-2 w-52 rounded-xl bg-white p-1.5 shadow-lg ring-1 ring-black/5 dark:bg-dark-800 dark:ring-white/10">
      <p class="px-2.5 py-2 text-xs font-medium text-gray-400 dark:text-dark-400">显示列</p>
      <button v-for="column in balanceColumns" :key="column.key" type="button" role="menuitemcheckbox" :aria-checked="!display.hidden_columns.includes(column.key)" class="display-option" @click="emit('toggle', column.key)"><span>{{ column.label }}</span><Icon v-if="!display.hidden_columns.includes(column.key)" name="check" size="sm" class="text-primary-600 dark:text-primary-400" /></button>
      <div class="my-1 border-t border-gray-100 dark:border-dark-700" />
      <button type="button" role="menuitemcheckbox" :aria-checked="display.compact" class="display-option" @click="emit('compact')"><span>紧凑行高</span><Icon v-if="display.compact" name="check" size="sm" class="text-primary-600 dark:text-primary-400" /></button>
      <button type="button" role="menuitem" class="display-option text-primary-600 dark:text-primary-400" @click="emit('reset'); close()">恢复默认视图</button>
    </div>
  </div>
</template>

<style scoped>
.display-option { @apply flex min-h-9 w-full items-center justify-between gap-3 rounded-lg px-2.5 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:text-gray-200 dark:hover:bg-dark-700; }
</style>
