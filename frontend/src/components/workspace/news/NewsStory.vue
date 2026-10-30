<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import type { NewsItem } from '@/api/workspace'
import newsIcons from '@/assets/workspace/news-icons.svg'
const props = defineProps<{ item: NewsItem; saved: boolean; busy: boolean }>()
const emit = defineEmits<{ favorite: [] }>()
const surface = ref<HTMLElement | null>(null), rows = ref(260)
let observer: ResizeObserver | undefined
const articleURL = computed(() => 'https://news.macos.do/api/v1/news/' + props.item.id + '/open')
const colors: Record<string, string> = { code: 'green', apps: 'blue', brain: 'indigo', sparkles: 'violet', cpu: 'orange', settings: 'orange', cloud: 'cyan', 'brand-javascript': 'orange', 'brand-github': 'slate' }
function color(icon?: string) { return 'var(--news-' + (colors[icon || ''] || 'slate') + ')' }
function sprite(icon?: string) { return newsIcons + '#tabler-' + (/^[a-z0-9-]+$/.test(icon || '') ? icon : 'tag') }
function relativeTime(value: number) {
  const delta = (value - Date.now()) / 1000
  const format = new Intl.RelativeTimeFormat('zh-CN', { numeric: 'auto' })
  for (const [unit, seconds] of [['year', 31536000], ['month', 2592000], ['day', 86400], ['hour', 3600], ['minute', 60]] as const)
    if (Math.abs(delta) >= seconds || unit === 'minute') return format.format(Math.round(delta / seconds), unit)
  return '刚刚'
}
onMounted(() => {
  if (!surface.value) return
  observer = new ResizeObserver(() => { if (surface.value) rows.value = Math.ceil(surface.value.getBoundingClientRect().height + 16) })
  observer.observe(surface.value)
})
onBeforeUnmount(() => observer?.disconnect())
</script>

<template>
  <article class="news-card-shell news-story" :aria-label="item.title" :style="{ gridRowEnd: 'span ' + rows, '--news-accent': color(item.category?.iconId) }">
    <div ref="surface" class="news-card-surface story-surface">
      <header class="story-meta">
        <div class="story-origin">
          <span v-if="item.category" class="story-category"><svg aria-hidden="true" fill="none" stroke="currentColor" viewBox="0 0 24 24" :style="{ color: color(item.category.iconId) }"><use :href="sprite(item.category.iconId)" /></svg>{{ item.category.name }}</span>
          <span class="story-source">{{ item.source.name }}</span>
        </div>
        <time :datetime="new Date(item.publishedAt).toISOString()" :title="new Date(item.publishedAt).toLocaleString('zh-CN')">{{ relativeTime(item.publishedAt) }}</time>
        <button class="bookmark-button" :class="{ saved }" :disabled="busy" :aria-label="saved ? '取消收藏' : '收藏新闻'" :aria-pressed="saved" @click="emit('favorite')"><Icon name="bookmark" size="xs" /></button>
      </header>
      <h2 class="story-title font-heading"><a class="news-card-link" :href="articleURL" target="_blank" rel="noopener noreferrer">{{ item.title }}<Icon name="externalLink" size="xs" /></a></h2>
      <p class="story-summary">{{ item.summary }}</p>
      <footer class="story-tags"><span v-for="tag in item.tags" :key="tag.id"><svg aria-hidden="true" fill="none" stroke="currentColor" viewBox="0 0 24 24" :style="{ color: color(tag.iconId) }"><use :href="sprite(tag.iconId)" /></svg>{{ tag.name }}</span></footer>
    </div>
  </article>
</template>

<style scoped>
.news-story { grid-column-start: auto; min-width: 0; padding-bottom: 16px; }
.story-surface { padding: 16px; border-width: 1px; border-style: solid; border-radius: 16px; background: var(--card); color: var(--card-foreground); }
.story-meta { display: flex; align-items: center; gap: 6px; color: var(--muted-foreground); font-size: 12px; min-width: 0; }
.story-origin { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; flex: 1; min-width: 0; }
.story-category, .story-tags > span { display: inline-flex; align-items: center; gap: 4px; font-size: 12px; font-weight: 500; }
.story-category svg, .story-tags svg { width: 14px; height: 14px; flex-shrink: 0; }
.story-source { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.story-meta time { white-space: nowrap; font-variant-numeric: tabular-nums; }
.bookmark-button { display: grid; place-items: center; width: 24px; height: 24px; flex-shrink: 0; border-radius: 12px; color: var(--muted-foreground); }
.bookmark-button:hover, .bookmark-button.saved { background: var(--muted); color: var(--foreground); }
.bookmark-button:disabled { opacity: .5; }
.bookmark-button:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
.story-title { font-size: 16px; line-height: 1.375; font-weight: 500; margin: 8px 0 6px; overflow-wrap: anywhere; }
.story-title svg { display: inline; margin-left: 6px; vertical-align: -.05em; color: var(--muted-foreground); }
.story-summary { color: var(--muted-foreground); font-size: 14px; line-height: 1.625; overflow-wrap: anywhere; margin: 0; }
.story-tags { display: flex; flex-wrap: wrap; gap: 8px 12px; margin-top: 10px; color: var(--muted-foreground); }
</style>
