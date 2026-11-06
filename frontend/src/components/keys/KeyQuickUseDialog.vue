<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, shallowRef, watch, type CSSProperties } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ApiKey } from '@/types'
import ccSwitchIcon from '@/assets/icons/client-import/cc-switch.svg'
import magpieIcon from '@/assets/icons/client-import/magpie.svg'
import CcSwitchImportDialog from './CcSwitchImportDialog.vue'
import MagpieImportDialog from './MagpieImportDialog.vue'
import { getMagpieFormats } from '@/utils/magpieImport'

const props = defineProps<{ apiKey: ApiKey | null; baseUrl: string; siteName: string; anchor?: HTMLElement | null }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const method = shallowRef<'ccswitch' | 'magpie' | null>(null)
const menu = shallowRef<HTMLElement | null>(null)
const position = shallowRef<CSSProperties>({})
const magpieAvailable = computed(() => getMagpieFormats(props.apiKey?.group ?? null).length > 0)
function updatePosition() {
  if (method.value || !props.anchor) return
  const rect = props.anchor.getBoundingClientRect()
  const width = Math.min(224, window.innerWidth - 16)
  const left = Math.max(8, Math.min(rect.left, window.innerWidth - width - 8))
  const height = menu.value?.offsetHeight || 108
  position.value = {
    width: `${width}px`, left: `${left}px`,
    ...(window.innerHeight - rect.bottom < height && rect.top > window.innerHeight - rect.bottom
      ? { bottom: `${window.innerHeight - rect.top + 4}px` } : { top: `${rect.bottom + 4}px` })
  }
}
function close() { props.anchor?.focus(); emit('close') }
function outside(event: PointerEvent) {
  if (!method.value && event.target instanceof Node && !menu.value?.contains(event.target) && !props.anchor?.contains(event.target)) emit('close')
}
async function showMenu() {
  method.value = null
  await nextTick()
  updatePosition()
  menu.value?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus()
}
function keydown(event: KeyboardEvent) {
  if (method.value) return
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(); return }
  if (event.key === 'Tab') { emit('close'); return }
  if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const buttons = [...(menu.value?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') || [])]
  const index = buttons.indexOf(document.activeElement as HTMLButtonElement)
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : (index + (event.key === 'ArrowUp' ? -1 : 1) + buttons.length) % buttons.length
  buttons[next]?.focus()
}
watch(() => [props.apiKey, props.anchor], showMenu)
onMounted(() => {
  void showMenu()
  document.addEventListener('pointerdown', outside)
  document.addEventListener('keydown', keydown)
  window.addEventListener('resize', updatePosition)
  window.addEventListener('scroll', updatePosition, true)
})
onUnmounted(() => {
  document.removeEventListener('pointerdown', outside)
  document.removeEventListener('keydown', keydown)
  window.removeEventListener('resize', updatePosition)
  window.removeEventListener('scroll', updatePosition, true)
})
</script>

<template>
  <Teleport to="body">
    <div v-if="apiKey && !method" ref="menu" role="menu" :aria-label="t('keys.quickUse.title')" data-test="quick-use-menu" :style="position" class="animate-in fade-in slide-in-from-top-2 fixed z-[100000020] max-w-[calc(100vw-16px)] overflow-hidden rounded-xl bg-white shadow-lg ring-1 ring-black/5 duration-200 dark:bg-dark-800 dark:ring-white/10">
      <div class="p-1.5">
        <button type="button" role="menuitem" class="flex w-full items-center gap-2.5 rounded-lg border-b border-gray-100 px-3 py-2.5 text-left text-sm text-gray-700 transition-colors hover:bg-gray-100 focus-visible:bg-gray-100 focus-visible:outline-none dark:border-dark-700 dark:text-dark-100 dark:hover:bg-dark-700 dark:focus-visible:bg-dark-700" data-test="choose-ccswitch" @click="method = 'ccswitch'">
          <img :src="ccSwitchIcon" alt="" class="h-5 w-5 shrink-0 object-contain" /><span>{{ t('keys.importToCcSwitch') }}</span>
        </button>
        <button type="button" role="menuitem" :disabled="!magpieAvailable" class="flex w-full items-center gap-2.5 rounded-lg px-3 py-2.5 text-left text-sm text-gray-700 transition-colors hover:bg-gray-100 focus-visible:bg-gray-100 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:text-dark-100 dark:hover:bg-dark-700 dark:focus-visible:bg-dark-700" data-test="choose-magpie" @click="method = 'magpie'">
          <img :src="magpieIcon" alt="" class="h-5 w-5 shrink-0 object-contain dark:invert" /><span class="flex-1">{{ t('keys.magpieImport.title') }}</span>
          <span v-if="!magpieAvailable" class="text-xs text-gray-500 dark:text-dark-400">{{ t('keys.quickUse.unavailable') }}</span>
        </button>
      </div>
    </div>
  </Teleport>
  <CcSwitchImportDialog v-if="method === 'ccswitch'" :api-key="apiKey" :base-url="baseUrl" :site-name="siteName" show-back @back="showMenu" @close="close" />
  <MagpieImportDialog v-else-if="method === 'magpie'" :api-key="apiKey" :base-url="baseUrl" :site-name="siteName" @back="showMenu" @close="close" />
</template>
