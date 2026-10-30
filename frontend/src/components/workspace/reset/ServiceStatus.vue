<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import DOMPurify from 'dompurify'
import { workspaceAPI } from '@/api/workspace'
import { buildOfficialStack, getOfficialWindow, type OfficialStatusData } from '@/utils/originalServiceStatus'
const data = ref<OfficialStatusData | null>(null), error = ref(false)
const markup = computed(() => data.value ? DOMPurify.sanitize(buildOfficialStack(data.value), { USE_PROFILES: { html: true, svg: true } }) : '')
let request: AbortController | null = null
let timer: ReturnType<typeof setInterval> | undefined
async function refresh() {
  request?.abort()
  const current = new AbortController()
  request = current
  try {
    const window = getOfficialWindow()
    const [status, history] = await Promise.all([
      workspaceAPI.serviceStatus(current.signal),
      workspaceAPI.serviceHistory(window.start, window.end, current.signal)
    ])
    if (current.signal.aborted) return
    data.value = { summary: status.summary, history, window, fetchedAt: status.feedState.fetchedAt }
    error.value = false
  } catch {
    if (!current.signal.aborted) error.value = true
  }
}
function toggleGroup(event: MouseEvent) {
  if (!(event.target instanceof Element)) return
  const button = event.target.closest('.ikun-openai-group-header')
  const group = button?.closest('[data-openai-group]')
  if (!button || !group) return
  button.setAttribute('aria-expanded', String(group.classList.toggle('is-expanded')))
}
onMounted(() => {
  void refresh()
  timer = setInterval(() => { if (document.visibilityState === 'visible') void refresh() }, 300000)
})
onBeforeUnmount(() => { request?.abort(); clearInterval(timer) })
</script>

<template>
  <div id="ikun-openai-status-host">
    <div v-if="!data" class="ikun-openai-loading" role="status">{{ error ? '官方状态暂时无法更新。' : '正在读取 OpenAI 官方状态…' }}<button v-if="error" class="ml-2 underline" @click="refresh">重试</button></div>
    <!-- 原页面的状态卡片经过清洗后渲染，保留原排版与展开交互。 -->
    <div v-else @click="toggleGroup" v-html="markup" />
  </div>
</template>
