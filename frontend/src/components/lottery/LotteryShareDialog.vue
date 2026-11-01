<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import WorkspaceDialog from '@/components/workspace/WorkspaceDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { createLotteryPoster } from '@/utils/lotteryPoster'
import { useAppStore } from '@/stores/app'
import type { LotteryWin } from '@/api/lottery'
const props = defineProps<{ win: LotteryWin | null }>()
const emit = defineEmits<{ close: [] }>()
const appStore = useAppStore()
const url = ref(''),
  message = ref(''),
  busy = ref(false)
let blob: Blob | null = null,
  generation = 0
function revoke() {
  if (url.value) URL.revokeObjectURL(url.value)
  url.value = ''
  blob = null
}
watch(
  () => props.win,
  async (win) => {
    const current = ++generation
    revoke()
    message.value = ''
    if (!win) return
    busy.value = true
    try {
      const image = await createLotteryPoster(win, appStore.siteName)
      if (current !== generation) return
      blob = image
      url.value = URL.createObjectURL(image)
    } catch (e) {
      if (current === generation)
        message.value = e instanceof Error ? e.message : '海报暂时无法生成。'
    } finally {
      if (current === generation) busy.value = false
    }
  },
  { immediate: true }
)
async function copy() {
  if (!blob) return
  try {
    if (!navigator.clipboard?.write || typeof ClipboardItem === 'undefined')
      throw new Error('当前浏览器不支持复制图片，请保存海报。')
    await navigator.clipboard.write([new ClipboardItem({ 'image/png': blob })])
    message.value = '图片已复制，可以直接粘贴分享'
  } catch (e) {
    message.value =
      e instanceof Error && e.message.includes('不支持')
        ? e.message
        : '浏览器未允许复制图片，请使用保存海报。'
  }
}
function save() {
  if (!url.value || !props.win) return
  const a = document.createElement('a')
  a.href = url.value
  a.download = `${appStore.siteName.replace(/[\\/:*?"<>|]/g, "_")}-中奖-${props.win.id}.png`
  a.click()
}
onBeforeUnmount(() => {
  generation++
  revoke()
})
</script>
<template>
  <WorkspaceDialog :open="!!win" title="中奖海报" @update:open="emit('close')">
    <div class="lottery-poster-preview">
      <p v-if="busy" role="status">正在生成你的中奖海报…</p>
      <img v-if="url" :src="url" alt="包含邮箱、用户名、中奖金额和累计中奖次数的中奖海报" />
    </div>
    <p class="lottery-muted lottery-share-notice">海报会展示完整邮箱和用户名，分享前请确认。</p>
    <p v-if="message" class="lottery-feedback-text" role="status">{{ message }}</p>
    <template #footer
      ><button class="btn btn-secondary lottery-button" :disabled="!url" @click="save">
        <Icon name="download" size="sm" />保存海报</button
      ><button
        class="btn btn-secondary lottery-button lottery-primary"
        :disabled="!url"
        @click="copy"
      >
        <Icon name="copy" size="sm" />复制图片
      </button></template
    >
  </WorkspaceDialog>
</template>
