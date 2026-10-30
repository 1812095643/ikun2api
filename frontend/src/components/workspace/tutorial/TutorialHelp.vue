<script setup lang="ts">
import { computed, shallowRef, watch } from 'vue'
import { useRoute } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import TutorialBody from './TutorialBody.vue'
import { useClipboard } from '@/composables/useClipboard'
import { tutorialFaqs, tutorialFeedback, tutorialHref, tutorialText } from '@/utils/tutorial'

const route = useRoute()
const query = shallowRef('')
const category = shallowRef('全部问题')
const { copyToClipboard } = useClipboard()
const categories = ['全部问题', ...new Set(tutorialFaqs.map(item => item.category))]
const searchable = tutorialFaqs.map(item => ({
  ...item, searchText: tutorialText(`${item.question} ${item.answer}`).toLowerCase()
}))
const questions = computed(() => searchable.filter(item =>
  (category.value === '全部问题' || item.category === category.value) &&
  item.searchText.includes(query.value.trim().toLowerCase())
))

watch(() => route.hash, hash => {
  if (hash.startsWith('#faq-')) {
    query.value = ''
    category.value = '全部问题'
  }
})
</script>

<template>
  <div class="tutorial-help">
    <div class="tutorial-help-shortcuts">
      <RouterLink :to="tutorialHref('start')">第一次使用？从快速开始读起 <Icon name="arrowRight" size="sm" /></RouterLink>
      <RouterLink :to="tutorialHref('errors')">接入遇到问题？按错误码排查 <Icon name="arrowRight" size="sm" /></RouterLink>
    </div>
    <label class="tutorial-search tutorial-faq-search">
      <Icon name="search" size="sm" />
      <input v-model="query" type="search" aria-label="查找常见问题" placeholder="例如：导入没有反应、401、余额、工单…">
    </label>
    <div class="tutorial-faq-categories" aria-label="问题分类">
      <button
        v-for="item in categories"
        :key="item"
        type="button"
        :aria-pressed="category === item"
        @click="category = item"
      >{{ item }}</button>
    </div>
    <div aria-live="polite">
      <p class="tutorial-result-count">共 {{ questions.length }} 个问题</p>
      <details v-for="item in questions" :id="`faq-${item.id}`" :key="item.id" class="tutorial-faq-item">
        <summary>{{ item.question }}<Icon name="plus" size="sm" /></summary>
        <TutorialBody :html="item.answer" />
      </details>
      <p v-if="!questions.length" class="tutorial-empty">暂时没有匹配的问题，可以换个关键词，或切换到全部问题。</p>
    </div>
    <section class="tutorial-support">
      <h2>还没有找到答案？</h2>
      <p>在工单中写清发生时间、客户端版本和脱敏报错。提交后可在同一张工单里查看回复、补充信息。</p>
      <div class="ws-actions">
        <a href="/tickets" target="_blank" rel="noopener noreferrer" class="ws-button ws-button-primary">提交工单<Icon name="arrowRight" size="sm" /></a>
        <button type="button" class="ws-button" @click="copyToClipboard(tutorialFeedback)"><Icon name="copy" size="sm" />复制排查信息模板</button>
      </div>
    </section>
  </div>
</template>
