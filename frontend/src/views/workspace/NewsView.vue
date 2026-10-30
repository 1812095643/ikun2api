<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import WorkspaceShell from '@/components/workspace/WorkspaceShell.vue'
import FeedStatus from '@/components/workspace/FeedStatus.vue'
import NewsFilters from '@/components/workspace/news/NewsFilters.vue'
import NewsStory from '@/components/workspace/news/NewsStory.vue'
import { useNews } from '@/composables/workspace/useNews'
import '@/styles/news-original.css'

const { view, search, filters, sourceChoice, visible, categories, tags, sources, favorites,
  favoriteIds, loading, error, hasMore, feedState, saving, load, toggleFavorite } = useNews()
const tabs = [
  { value: 'latest', label: '最新' }, { value: 'hot', label: '热门' },
  { value: 'recommended', label: '推荐' }, { value: 'favorites', label: '收藏' }
]
</script>

<template>
  <WorkspaceShell class="workspace-discovery workspace-news-original">
    <div class="news-original">
    <section class="news-board news-outlet-surface">
      <div class="news-topline">
        <div class="news-title-block"><span class="news-feed-mark"><Icon name="globe" size="xs" />实时聚合</span><h2 class="news-heading">时事新闻</h2></div>
        <div class="ws-actions">
          <FeedStatus :state="feedState" :loading="loading" />
          <button class="ws-icon-btn" :disabled="loading" aria-label="刷新新闻" @click="load()">
            <Icon name="refresh" size="sm" :class="{ 'ws-spin': loading }" />
          </button>
        </div>
      </div>
      <NewsFilters v-model:search="search" v-model:category-id="filters.categoryId"
        v-model:tag-id="filters.tagId" v-model:source-choice="sourceChoice"
        :categories="categories" :tags="tags" :sources="sources" />
      <div class="news-tabs-row">
        <div class="ws-tabs" role="tablist" aria-label="新闻视图">
          <button v-for="tab in tabs" :key="tab.value" role="tab" class="ws-tab"
            :aria-selected="view === tab.value" @click="view = tab.value">
            {{ tab.label }}<span v-if="tab.value === 'favorites' && favorites.length">{{ favorites.length }}</span>
          </button>
        </div>
        <span v-if="visible.length" class="news-count">{{ visible.length }} 篇</span>
      </div>
      <div v-if="error" class="ws-error" role="alert">{{ error }}<button @click="load()">重试</button></div>
      <div v-if="loading && !visible.length" class="ws-empty" role="status">
        <Icon name="refresh" class="ws-spin" /><p>正在读取新闻…</p>
      </div>
      <div v-else-if="!visible.length && !error" class="ws-empty">
        <Icon name="bookmark" size="lg" />
        <h2>{{ view === 'favorites' ? '暂无收藏' : '没有匹配的新闻' }}</h2>
        <p>{{ view === 'favorites' ? '点击文章右上角即可收藏。' : '试试其他筛选条件。' }}</p>
      </div>
      <div class="news-grid" aria-label="新闻列表">
        <NewsStory v-for="item in visible" :key="item.id" :item="item"
          :saved="favoriteIds.has(item.id)" :busy="saving.has(item.id)" @favorite="toggleFavorite(item)" />
      </div>
      <div v-if="hasMore && view !== 'favorites'" class="news-more">
        <button class="ws-button" :disabled="loading" @click="load(true)">{{ loading ? '读取中…' : '加载更多' }}</button>
      </div>
    </section>
    </div>
  </WorkspaceShell>
</template>

<style scoped>
.news-original { font-family: Inter, "PingFang SC", "Microsoft YaHei", sans-serif; color: var(--foreground); }
.news-board { padding: 20px; border-width: 1px; border-style: solid; border-radius: 16px; }
.news-title-block { display: grid; gap: 6px; }
.news-feed-mark { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; font-weight: 500; }
.news-heading { font-size: 16px; font-weight: 500; line-height: 1.4; }
.news-topline { display: flex; justify-content: space-between; align-items: center; gap: 16px; margin-bottom: 22px; }
.news-mark { display: inline-flex; align-items: center; gap: 8px; color: var(--ws-primary); font-size: 15px; font-weight: 650; white-space: nowrap; }
.news-topline .ws-icon-btn { border-color: transparent; background: transparent; }
.news-tabs-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin: 15px 0 20px; border-bottom: 1px solid var(--ws-line); }
.ws-tabs { border: 0; margin: 0; }
.ws-tab { padding: 13px 16px; }
.ws-tab:first-child { padding-left: 8px; }
.ws-tab:first-child::after { left: 8px; }
.ws-tab span { margin-left: 6px; font-size: 10px; }
.news-count { color: var(--ws-muted); font-size: 11px; white-space: nowrap; }
.news-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 300px), 1fr)); grid-auto-rows: 1px; grid-auto-flow: dense; column-gap: 16px; row-gap: 0; align-items: start; }
.news-more { padding-top: 26px; text-align: center; }
@media (max-width: 600px) { .news-topline { align-items: flex-start; } .news-topline .ws-actions { justify-content: flex-end; gap: 3px; } .news-mark { padding-top: 7px; } }
</style>
