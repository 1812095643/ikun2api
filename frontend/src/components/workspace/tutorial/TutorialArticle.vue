<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import { useAppStore } from '@/stores/app'
import Icon from '@/components/icons/Icon.vue'
import TutorialBody from './TutorialBody.vue'
import TutorialHelp from './TutorialHelp.vue'
import { useClipboard } from '@/composables/useClipboard'
import { tutorialHref, type TutorialDocument } from '@/utils/tutorial'

const props = defineProps<{
  document: TutorialDocument
  previous?: TutorialDocument
  next?: TutorialDocument
}>()
const appStore = useAppStore()
const article = shallowRef<HTMLElement | null>(null)
const { copyToClipboard } = useClipboard()
const description = computed(() => props.document.description.split('__IKUN_SITE_NAME__').join(appStore.siteName || 'IKUN2api'))
</script>

<template>
  <article ref="article" class="tutorial-main" tabindex="-1">
    <header class="tutorial-intro">
      <div class="tutorial-eyebrow">
        <span>{{ document.category }}</span>
        <button type="button" class="tutorial-copy-page" @click="copyToClipboard(article?.innerText || '')"><Icon name="copy" size="sm" />复制本页</button>
      </div>
      <h1>{{ document.title }}</h1>
      <p>{{ description }}</p>
      <span v-if="document.minutes" class="tutorial-reading-time"><Icon name="clock" size="sm" />约 {{ document.minutes }} 分钟阅读</span>
    </header>
    <TutorialHelp v-if="document.help" />
    <TutorialBody v-else :key="document.id" :html="document.body" />
    <nav v-if="!document.help" class="tutorial-pagination" aria-label="前后文章">
      <RouterLink v-if="previous" :to="tutorialHref(previous.id)"><Icon name="arrowLeft" size="sm" /><span><small>上一篇</small>{{ previous.title }}</span></RouterLink>
      <RouterLink v-if="next" :to="tutorialHref(next.id)" class="tutorial-next"><span><small>下一篇</small>{{ next.title }}</span><Icon name="arrowRight" size="sm" /></RouterLink>
    </nav>
  </article>
</template>
