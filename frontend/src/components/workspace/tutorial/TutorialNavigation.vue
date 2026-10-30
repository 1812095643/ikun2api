<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import { useEventListener, useMediaQuery } from '@vueuse/core'
import Icon from '@/components/icons/Icon.vue'
import { tutorialHref, type TutorialDocument, type TutorialFaq } from '@/utils/tutorial'

const props = defineProps<{
  documents: TutorialDocument[]
  questions: TutorialFaq[]
  activeId: string
  query: string
}>()
const emit = defineEmits<{ 'update:query': [value: string] }>()
const expanded = shallowRef(false)
const searchInput = shallowRef<HTMLInputElement | null>(null)
const wide = useMediaQuery('(min-width: 901px)')
const categories = computed(() => [...new Set(props.documents.map(item => item.category))])
const hasResults = computed(() => props.documents.length + props.questions.length > 0)

function chooseArticle() {
  expanded.value = false
  emit('update:query', '')
}

useEventListener(window, 'keydown', event => {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k' && !event.altKey) {
    event.preventDefault()
    searchInput.value?.focus()
  }
})
</script>

<template>
  <aside class="tutorial-navigation" aria-label="新手教程目录">
    <label class="tutorial-search">
      <Icon name="search" size="sm" />
      <input
        ref="searchInput"
        :value="query"
        type="search"
        aria-label="搜索教程与常见问题"
        placeholder="搜索教程或问题"
        @input="emit('update:query', ($event.target as HTMLInputElement).value)"
      >
      <kbd>⌘ / Ctrl K</kbd>
    </label>
    <button
      type="button"
      class="tutorial-directory-toggle"
      aria-controls="tutorial-directory"
      :aria-expanded="wide || expanded || Boolean(query)"
      @click="expanded = !expanded"
    >
      <Icon name="book" size="sm" />
      教程目录
      <Icon name="chevronDown" size="sm" :class="{ 'rotate-180': expanded || Boolean(query) }" />
    </button>
    <nav v-show="wide || expanded || Boolean(query)" id="tutorial-directory" class="tutorial-directory">
      <p v-if="query" class="tutorial-result-count">找到 {{ documents.length + questions.length }} 项内容</p>
      <section v-for="category in categories" :key="category" class="tutorial-nav-group">
        <h2>{{ category }}</h2>
        <RouterLink
          v-for="item in documents.filter(document => document.category === category)"
          :key="item.id"
          :to="tutorialHref(item.id)"
          class="tutorial-nav-link"
          :class="{ 'is-active': item.id === activeId }"
          :aria-current="item.id === activeId ? 'page' : undefined"
          @click="chooseArticle"
        >
          {{ item.title }}
        </RouterLink>
      </section>
      <section v-if="questions.length" class="tutorial-nav-group">
        <h2>常见问题</h2>
        <RouterLink
          v-for="item in questions"
          :key="item.id"
          :to="tutorialHref('help', `faq-${item.id}`)"
          class="tutorial-nav-link"
          @click="chooseArticle"
        >
          {{ item.question }}
        </RouterLink>
      </section>
      <p v-if="!hasResults" class="tutorial-empty">暂时没有匹配的内容，试试“密钥”“地址”或错误码。</p>
    </nav>
  </aside>
</template>
