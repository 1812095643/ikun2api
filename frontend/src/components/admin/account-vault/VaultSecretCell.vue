<script setup lang="ts">
import { shallowRef, onMounted, onBeforeUnmount } from 'vue'
import { useClipboard } from '@/composables/useClipboard'
import { useAppStore } from '@/stores/app'
const props = defineProps<{ id: string; field: string; present: boolean; read: (id: string, field: string) => Promise<string> }>()
const value = shallowRef(''), busy = shallowRef(false)
let timer: ReturnType<typeof setTimeout> | undefined
let alive = true
const { copyToClipboard } = useClipboard(), app = useAppStore()
function clear() { value.value = ''; if (timer) clearTimeout(timer) }
async function act(copy: boolean) {
 if (busy.value || !props.present) return
 if (!copy && value.value) { clear(); return }
 busy.value = true
 try {
  const text = await props.read(props.id, props.field)
  if (!alive) return
  if (copy) { if (await copyToClipboard(text)) app.showSuccess('已复制') }
  else { value.value = text; timer = setTimeout(clear, 60000) }
 } catch { /* shared authorization flow displays a sanitized error */ }
 finally { busy.value = false }
}
function visibility() { if (document.hidden) clear() }
onMounted(() => document.addEventListener('visibilitychange', visibility))
onBeforeUnmount(() => { alive = false; clear(); document.removeEventListener('visibilitychange', visibility) })
</script>
<template>
 <div v-if="present" class="flex min-w-[100px] max-w-[240px] items-center gap-2">
  <button type="button" class="min-w-0 flex-1 truncate text-left font-mono text-xs text-gray-700 hover:text-primary-600 dark:text-gray-200" :disabled="busy" :aria-label="'复制 ' + field" @click="act(true)">{{ value || '••••••••' }}</button>
  <button type="button" class="shrink-0 text-xs text-gray-500 hover:text-primary-600 dark:text-gray-400" :disabled="busy" :aria-label="(value ? '隐藏 ' : '查看 ') + field" @click="act(false)">{{ busy ? '…' : value ? '隐藏' : '查看' }}</button>
 </div><span v-else class="text-gray-400">—</span>
</template>
