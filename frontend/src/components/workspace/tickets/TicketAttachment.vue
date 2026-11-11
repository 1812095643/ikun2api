<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { ticketAPI, type TicketImage } from '@/api/workspace'
import WorkspaceDialog from '../WorkspaceDialog.vue'
const props = defineProps<{ ticketId: number; image: TicketImage; admin: boolean }>()
const emit = defineEmits<{ remove: [image: TicketImage] }>()
const url = ref(''),
  failed = ref(false),
  preview = ref(false)
let request: AbortController | null = null
async function loadImage() {
  request?.abort()
  failed.value = false
  if (props.image.deleted) return
  const current = new AbortController()
  request = current
  try {
    const blob = await ticketAPI(props.admin).image(props.ticketId, props.image.id, current.signal)
    if (!current.signal.aborted) url.value = URL.createObjectURL(blob)
  } catch {
    if (!current.signal.aborted) failed.value = true
  }
}
watch(
  [() => props.image.id, () => props.image.deleted, () => props.ticketId, () => props.admin],
  () => {
    request?.abort()
    preview.value = false
    if (url.value) URL.revokeObjectURL(url.value)
    url.value = ''
    void loadImage()
  },
  { immediate: true }
)
onBeforeUnmount(() => {
  request?.abort()
  if (url.value) URL.revokeObjectURL(url.value)
})
</script>

<template>
  <div class="ticket-attachment">
    <span
      v-if="image.deleted"
      class="ws-muted"
      >图片已由管理员删除</span
    >
    <button v-else-if="failed" type="button" class="attachment-preview ws-muted" @click="loadImage">读取失败 · 重试</button>
    <template v-else
      ><button
        type="button"
        class="attachment-preview"
        :aria-label="`查看图片：${image.name}`"
        :disabled="!url"
        @click="preview = true"
      >
        <img
          v-if="url"
          :src="url"
          :alt="image.name"
          loading="lazy"
        /><span
          v-else
          class="ws-muted"
          >读取图片…</span
        ></button
      ><button
        v-if="admin"
        class="attachment-delete"
        type="button"
        @click="emit('remove', image)"
      >
        删除已阅图片
      </button></template
    >
    <WorkspaceDialog
      v-model:open="preview"
      :title="image.name"
      wide
      ><img
        :src="url"
        :alt="image.name"
        class="max-h-[70vh] w-full object-contain"
    /></WorkspaceDialog>
  </div>
</template>

<style scoped>
.ticket-attachment {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
}
.attachment-preview {
  display: grid;
  place-items: center;
  width: 144px;
  height: 96px;
  flex: none;
  overflow: hidden;
  border-radius: 8px;
  background: var(--ws-soft);
  border: 1px solid var(--ws-line);
}
.attachment-preview img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.attachment-delete {
  color: var(--ws-muted);
  font-size: 11px;
  text-decoration: underline;
}
</style>
