<script setup lang="ts">
import TutorialHeader from '@/components/workspace/tutorial/TutorialHeader.vue'
import TutorialNavigation from '@/components/workspace/tutorial/TutorialNavigation.vue'
import TutorialArticle from '@/components/workspace/tutorial/TutorialArticle.vue'
import { useTutorial } from '@/composables/workspace/useTutorial'
import { tutorialHref } from '@/utils/tutorial'
import '@/styles/workspace.css'
import '@/styles/tutorial.css'

const { active, query, documents, questions, toc, previous, next } = useTutorial()
</script>

<template>
  <div class="workspace tutorial-site">
    <a class="tutorial-skip-link" href="#tutorial-content">跳转到正文</a>
    <TutorialHeader />
    <main id="tutorial-content" class="tutorial-layout" tabindex="-1">
      <TutorialNavigation v-model:query="query" :documents="documents" :questions="questions" :active-id="active.id" />
      <TutorialArticle :document="active" :previous="previous" :next="next" />
      <aside v-if="toc.length" class="tutorial-toc" aria-label="本页内容">
        <p>本页内容</p>
        <RouterLink v-for="item in toc" :key="item.id" :to="tutorialHref(active.id, item.id)">{{ item.title }}</RouterLink>
      </aside>
    </main>
  </div>
</template>
