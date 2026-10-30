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
watch(
  () => [props.image.id, props.image.deleted, props.ticketId],
  async () => {
    request?.abort()
    if (url.value) URL.revokeObjectURL(url.value)
    url.value = ''
    failed.value = false
    if (props.image.deleted) return
    const current = new AbortController()
    request = current
    try {
      const blob = await ticketAPI(props.admin).image(
        props.ticketId,
        props.image.id,
        current.signal
      )
      if (!current.signal.aborted) url.value = URL.createObjectURL(blob)
    } catch {
      if (!current.signal.aborted) failed.value = true
    }
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
    <span
      v-else-if="failed"
      class="ws-muted"
      >图片暂时无法读取</span
    >
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
  min-width: 80px;
  min-height: 60px;
  overflow: hidden;
  border-radius: 8px;
  background: var(--ws-soft);
  border: 1px solid var(--ws-line);
}
.attachment-preview img {
  width: 150px;
  height: 110px;
  object-fit: cover;
}
.attachment-delete {
  color: var(--ws-muted);
  font-size: 11px;
  text-decoration: underline;
}
</style>
