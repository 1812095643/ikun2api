import { computed, nextTick, shallowRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { tutorialDocuments, tutorialFaqs, tutorialHref, tutorialText } from '@/utils/tutorial'
import { useAuthStore } from '@/stores/auth'
import { useNavigationStore } from '@/stores/navigation'

export function useTutorial() {
  const route = useRoute()
  const router = useRouter()
  const authStore = useAuthStore()
  const navigationStore = useNavigationStore()
  const query = shallowRef('')
  const searchableDocuments = tutorialDocuments.map(item => ({
    ...item, searchText: tutorialText(`${item.title} ${item.description} ${item.body}`).toLowerCase()
  }))
  const searchableFaqs = tutorialFaqs.map(item => ({
    ...item, searchText: tutorialText(`${item.question} ${item.answer}`).toLowerCase()
  }))
  const active = computed(() => tutorialDocuments.find(item => item.id === route.params.article) || tutorialDocuments[0])
  const normalizedQuery = computed(() => query.value.trim().toLowerCase())
  const documents = computed(() => searchableDocuments.filter(item => !normalizedQuery.value || item.searchText.includes(normalizedQuery.value)))
  const questions = computed(() => normalizedQuery.value ? searchableFaqs.filter(item => item.searchText.includes(normalizedQuery.value)) : [])
  const toc = computed(() => {
    if (active.value.help) return []
    const body = new DOMParser().parseFromString(active.value.body, 'text/html')
    return Array.from(body.querySelectorAll<HTMLHeadingElement>('h2[id]')).map(item => ({ id: item.id, title: item.textContent || '' }))
  })
  const activeIndex = computed(() => tutorialDocuments.findIndex(item => item.id === active.value.id))
  const previous = computed(() => tutorialDocuments[activeIndex.value - 1])
  const next = computed(() => tutorialDocuments[activeIndex.value + 1])

  // 文档独立展示后不再挂载控制台侧栏，已登录用户仍需确认教程更新已读。
  watch(() => authStore.user?.id, userID => {
    if (userID) void navigationStore.viewed('/tutorial')
  }, { immediate: true })

  watch(() => [route.params.article, route.hash], async () => {
    let hash = route.hash.slice(1)
    try {
      hash = decodeURIComponent(hash)
    } catch {
      hash = ''
    }
    const [legacyId, section] = hash.split('/')
    if (!route.params.article && tutorialDocuments.some(item => item.id === legacyId)) {
      await router.replace(tutorialHref(legacyId, section))
      return
    }
    if (route.params.article && !tutorialDocuments.some(item => item.id === route.params.article)) {
      await router.replace(tutorialHref())
      return
    }
    await nextTick()
    if (hash) {
      const element = document.getElementById(hash)
      if (element instanceof HTMLDetailsElement) element.open = true
    }
  }, { immediate: true, flush: 'post' })

  return { active, query, documents, questions, toc, previous, next }
}
