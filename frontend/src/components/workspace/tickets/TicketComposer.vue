<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import type { TicketDraft } from '@/api/workspace'
const props = defineProps<{
  busy?: boolean
  disabled?: boolean
  locked?: boolean
  placeholder?: string
  messageId?: string
  submitLabel?: string
  submit: (draft: TicketDraft) => Promise<boolean>
}>()
const content = ref(''),
  images = ref<{ name: string; data: string; preview: string }[]>([]),
  imageError = ref(''),
  processing = ref(false)
const input = ref<HTMLInputElement | null>(null)
function remove(index: number) {
  URL.revokeObjectURL(images.value[index].preview)
  images.value.splice(index, 1)
}
async function addFiles(files: File[]) {
  if (props.busy || props.locked) return
  imageError.value = ''
  processing.value = true
  try {
    for (const file of files) {
      if (images.value.length >= 4) {
        imageError.value = '每次最多添加 4 张图片。'
        break
      }
      if (!['image/jpeg', 'image/png', 'image/webp', 'image/gif'].includes(file.type)) {
        imageError.value = '请选择 JPG、PNG、WebP 或 GIF 图片。'
        continue
      }
      const bitmap = await createImageBitmap(file)
      try {
        if (bitmap.width * bitmap.height > 20000000) {
          imageError.value = '图片分辨率太大，请缩小后再上传。'
          continue
        }
        const scale = Math.min(1, 1920 / Math.max(bitmap.width, bitmap.height))
        const canvas = document.createElement('canvas')
        canvas.width = Math.max(1, Math.round(bitmap.width * scale))
        canvas.height = Math.max(1, Math.round(bitmap.height * scale))
        const context = canvas.getContext('2d')
        if (!context) throw new Error('无法处理图片')
        context.fillStyle = '#fff'
        context.fillRect(0, 0, canvas.width, canvas.height)
        context.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
        let data = ''
        for (const quality of [0.82, 0.7, 0.58, 0.46]) {
          data = canvas.toDataURL('image/jpeg', quality).split(',')[1]
          if (data.length * 0.75 <= 512 * 1024) break
        }
        if (data.length * 0.75 > 512 * 1024) {
          imageError.value = '图片压缩后仍超过 512 KB，请缩小后重试。'
          continue
        }
        images.value.push({ name: file.name, data, preview: URL.createObjectURL(file) })
      } finally {
        bitmap.close()
      }
    }
  } catch {
    imageError.value = '图片无法读取，请重新选择。'
  } finally {
    processing.value = false
    if (input.value) input.value.value = ''
  }
}
function onPaste(event: ClipboardEvent) {
  const files = Array.from(event.clipboardData?.files || [])
  if (files.length) {
    event.preventDefault()
    void addFiles(files)
  }
}
async function send() {
  if (props.disabled || props.busy || processing.value || (!content.value.trim() && !images.value.length)) return
  if (
    await props.submit({
      content: content.value,
      images: images.value.map(({ name, data }) => ({ name, data }))
    })
  ) {
    content.value = ''
    while (images.value.length) remove(0)
  }
}
onBeforeUnmount(() => images.value.forEach((image) => URL.revokeObjectURL(image.preview)))
</script>

<template>
  <form
    class="ticket-composer"
    @submit.prevent="send"
  >
    <label
      class="sr-only"
      :for="messageId || 'ticket-message'"
      >工单内容</label
    >
    <textarea
      :id="messageId || 'ticket-message'"
      v-model="content"
      class="ws-input"
      rows="5"
      maxlength="50000"
      :disabled="busy || locked"
      :placeholder="placeholder || '描述遇到的问题，可直接粘贴截图…'"
      @paste="onPaste"
      @keydown.ctrl.enter.prevent="send"
    />
    <div
      v-if="images.length"
      class="composer-images"
    >
      <div
        v-for="(image, index) in images"
        :key="image.preview"
      >
        <img
          :src="image.preview"
          :alt="image.name"
        /><button
          type="button"
          :aria-label="`移除${image.name}`"
          :disabled="busy || locked"
          @click="remove(index)"
        >
          <Icon
            name="x"
            size="xs"
          />
        </button>
      </div>
    </div>
    <p
      v-if="imageError"
      class="text-xs text-red-500"
      role="alert"
    >
      {{ imageError }}
    </p>
    <div class="composer-footer">
      <input
        ref="input"
        type="file"
        class="sr-only"
        accept="image/png,image/jpeg,image/webp,image/gif"
        multiple
        aria-label="添加图片"
        @change="(event) => addFiles(Array.from((event.target as HTMLInputElement).files || []))"
      />
      <button
        type="button"
        class="ws-button ws-button-quiet"
        :disabled="busy || locked || processing"
        @click="input?.click()"
      >
        <Icon
          name="plus"
          size="sm"
        />{{ processing ? '处理图片中…' : '添加图片' }}
      </button>
      <span class="ws-muted">{{ content.length.toLocaleString() }} / 50,000</span>
      <button
        class="ws-button ws-button-primary"
        :disabled="disabled || busy || processing || (!content.trim() && !images.length)"
      >
        <Icon
          v-if="busy"
          name="refresh"
          size="sm"
          class="ws-spin"
        />{{ busy ? '提交中…' : submitLabel || '发送回复' }}
      </button>
    </div>
  </form>
</template>

<style scoped>
.ticket-composer {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.ticket-composer textarea {
  resize: vertical;
  min-height: 118px;
  max-height: 400px;
  line-height: 1.7;
}
.composer-footer {
  display: flex;
  align-items: center;
  gap: 12px;
}
.composer-footer .ws-button-primary {
  margin-left: auto;
}
.composer-images {
  display: flex;
  gap: 10px;
}
.composer-images > div {
  position: relative;
}
.composer-images img {
  width: 72px;
  height: 64px;
  object-fit: cover;
  border-radius: 7px;
}
.composer-images button {
  position: absolute;
  right: -6px;
  top: -6px;
  border-radius: 50%;
  padding: 3px;
  background: var(--ws-text);
  color: var(--ws-surface);
}
@media (max-width: 600px) {
  .composer-footer {
    gap: 6px;
  }
  .composer-footer .ws-muted {
    display: none;
  }
}
</style>
