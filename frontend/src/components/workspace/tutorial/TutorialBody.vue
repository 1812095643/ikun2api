<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { tutorialApiBase, tutorialFeedback, tutorialHtml } from '@/utils/tutorial'

const props = defineProps<{ html: string }>()
const appStore = useAppStore()
const router = useRouter()
const { copyToClipboard } = useClipboard()
const safeHtml = computed(() => tutorialHtml(
  props.html,
  tutorialApiBase(appStore.apiBaseUrl, window.location.origin),
  appStore.siteName || 'IKUN2api'
))

function activateCodeTab(tab: HTMLButtonElement) {
  const block = tab.closest('.code-block')
  if (!block) return
  block.querySelectorAll<HTMLButtonElement>('[data-code-tab]').forEach(item => {
    const selected = item === tab
    item.setAttribute('aria-selected', String(selected))
    item.tabIndex = selected ? 0 : -1
    const panel = block.querySelector<HTMLElement>(`[id="${item.getAttribute('aria-controls')}"]`)
    if (panel) panel.hidden = !selected
  })
}

async function handleClick(event: MouseEvent) {
  const target = event.target instanceof Element ? event.target : null
  const tab = target?.closest<HTMLButtonElement>('[data-code-tab]')
  if (tab) {
    activateCodeTab(tab)
    return
  }
  const copy = target?.closest<HTMLButtonElement>('[data-copy-code], [data-copy], [data-feedback]')
  if (copy) {
    const text = copy.hasAttribute('data-feedback') ? tutorialFeedback :
      copy.getAttribute('data-copy') || copy.closest('.code-block')?.querySelector('pre:not([hidden]) code')?.textContent || ''
    await copyToClipboard(text, '已复制，可以粘贴到你的配置中')
    return
  }
  const anchor = target?.closest<HTMLAnchorElement>('a[href]')
  const href = anchor?.getAttribute('href')
  if (href?.startsWith('/') && !href.startsWith('//') && anchor?.target !== '_blank' && event.button === 0 && !event.ctrlKey && !event.metaKey && !event.shiftKey && !event.altKey) {
    event.preventDefault()
    await router.push(href)
  }
}

function handleKeydown(event: KeyboardEvent) {
  const tab = event.target instanceof HTMLButtonElement && event.target.hasAttribute('data-code-tab') ? event.target : null
  if (!tab || !['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  const tabs = Array.from(tab.closest('.code-tabs')?.querySelectorAll<HTMLButtonElement>('[data-code-tab]') || [])
  const index = tabs.indexOf(tab)
  const nextIndex = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 :
    (index + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length
  const next = tabs[nextIndex]
  if (next) {
    event.preventDefault()
    activateCodeTab(next)
    next.focus()
  }
}
</script>

<template>
  <div class="tutorial-body" @click="handleClick" @keydown="handleKeydown" v-html="safeHtml"></div>
</template>
