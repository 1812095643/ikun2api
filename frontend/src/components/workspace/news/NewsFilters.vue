<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import type { Taxonomy } from '@/api/workspace'
defineProps<{ categories: Taxonomy[]; tags: Taxonomy[]; sources: Taxonomy[] }>()
const search = defineModel<string>('search', { required: true })
const categoryId = defineModel<string>('categoryId', { required: true })
const tagId = defineModel<string>('tagId', { required: true })
const sourceChoice = defineModel<string>('sourceChoice', { required: true })
</script>

<template>
  <div class="news-filters">
    <label class="news-search">
      <Icon name="search" size="sm" />
      <input v-model="search" class="ws-input" placeholder="搜索标题或摘要" aria-label="搜索已加载新闻" />
    </label>
    <select v-model="categoryId" class="ws-input" aria-label="新闻分类">
      <option value="">全部分类</option>
      <option v-for="item in categories" :key="item.id" :value="String(item.id)">{{ item.name }}</option>
    </select>
    <select v-model="tagId" class="ws-input" aria-label="新闻标签">
      <option value="">全部标签</option>
      <option v-for="item in tags" :key="item.id" :value="String(item.id)">{{ item.name }}</option>
    </select>
    <select v-model="sourceChoice" class="ws-input" aria-label="新闻来源">
      <option value="">全部来源</option>
      <option v-for="item in sources" :key="String(item.type) + ':' + item.id" :value="String(item.type ?? '') + ':' + item.id">{{ item.name }}</option>
    </select>
  </div>
</template>

<style scoped>
.news-filters { display: grid; grid-template-columns: minmax(220px, 2.3fr) repeat(3, minmax(110px, 1fr)); gap: 10px; }
.news-search { position: relative; }
.news-search svg { position: absolute; top: 9px; left: 14px; color: var(--ws-muted); }
.news-search input { padding-left: 39px; }
.ws-input { border-radius: 16px; min-height: 32px; height: 32px; padding-block: 4px; background: color-mix(in srgb, var(--input) 50%, transparent); color: var(--foreground); border-color: transparent; }
@media (max-width: 900px) { .news-filters { grid-template-columns: repeat(3, minmax(0, 1fr)); } .news-search { grid-column: 1/-1; } }
@media (max-width: 520px) { .news-filters { grid-template-columns: repeat(2, minmax(0, 1fr)); } .news-filters > select:last-child { grid-column: 1/-1; } }
</style>
