<script setup lang="ts">
import { ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import WorkspaceShell from '@/components/workspace/WorkspaceShell.vue'
import WorkspaceDialog from '@/components/workspace/WorkspaceDialog.vue'
import FeedStatus from '@/components/workspace/FeedStatus.vue'
import ResetOverview from '@/components/workspace/reset/ResetOverview.vue'
import ResetCalendar from '@/components/workspace/reset/ResetCalendar.vue'
import ResetPostCard from '@/components/workspace/reset/ResetPostCard.vue'
import ServiceStatus from '@/components/workspace/reset/ServiceStatus.vue'
import '@/styles/reset-original.css'
import { useResetMonitor } from '@/composables/workspace/useResetMonitor'
import { safeExternalURL, workspaceAPI, workspaceError, type ResetPost } from '@/api/workspace'

const { summary, items, category, counts, loading, error, cursor, now, feedState, load } = useResetMonitor()
const tabs = [
  { value: 0, label: '全部动态' }, { value: 10, label: '额度重置' },
  { value: 20, label: '重置预告' }, { value: 30, label: '相关动态' }
]
const contextOpen = ref(false), contextLoading = ref(false), contextItems = ref<ResetPost[]>([]),
  contextError = ref(''), contextSource = ref('')
function collectPosts(value: unknown, found: ResetPost[] = []): ResetPost[] {
  if (!value || typeof value !== 'object' || found.length >= 20) return found
  if ('originalText' in value && 'id' in value) {
    const post = value as ResetPost
    if (!found.some(old => old.id === post.id)) found.push(post)
  }
  for (const child of Object.values(value)) if (child && typeof child === 'object') collectPosts(child, found)
  return found
}
async function showContext(post: ResetPost) {
  contextOpen.value = true
  contextLoading.value = true
  contextItems.value = []
  contextError.value = ''
  contextSource.value = post.sourceUrl
  try {
    contextItems.value = collectPosts(await workspaceAPI.resetContext(post.id, 'reply-context'))
  } catch (e) {
    contextError.value = workspaceError(e)
  } finally {
    contextLoading.value = false
  }
}
</script>

<template>
  <WorkspaceShell class="workspace-discovery workspace-reset-original">
    <div class="reset-original">
    <div class="reset-toolbar">
      <FeedStatus :state="feedState" :loading="loading" />
      <button class="ws-button" :disabled="loading" @click="load()"><Icon name="refresh" size="sm" :class="{ 'ws-spin': loading }" />刷新</button>
    </div>
    <div v-if="error" class="ws-error" role="alert">{{ error }}<button @click="load()">重试</button></div>
    <ResetOverview v-if="summary" :summary="summary" :now="now" />
    <div class="original-shell-contentGrid">
      <section class="original-feed-timelineSection original-feed-surface">
        <div class="original-liquid-glass-ui-filterTabs" role="tablist" aria-label="重置动态分类">
          <button v-for="tab in tabs" :key="tab.value" class="original-liquid-glass-ui-filterTab" role="tab" :aria-selected="category === tab.value" :aria-pressed="category === tab.value" @click="category = tab.value">
            {{ tab.label }}<span v-if="tab.value">{{ counts.find(item => item.category === tab.value)?.count ?? '' }}</span>
          </button>
        </div>
        <div v-if="loading && !items.length" class="ws-empty" role="status"><Icon name="refresh" class="ws-spin" /><p>正在读取动态…</p></div>
        <ResetPostCard v-for="post in items" :key="post.id" :post="post" @context="showContext" />
        <div v-if="!loading && !items.length && !error" class="ws-empty"><h2>暂无匹配的动态</h2><p>试试其他分类。</p></div>
        <div v-if="cursor" class="reset-more"><button class="ws-button" :disabled="loading" @click="load(true)">{{ loading ? '读取中…' : '加载更多' }}</button></div>
      </section>
      <aside v-if="summary" class="original-calendar-sidebar">
        <ResetCalendar :events="summary.recentResets" :updated-at="feedState?.fetchedAt ? new Date(feedState.fetchedAt).toISOString() : summary.updatedAt" />
        <ServiceStatus />
      </aside>
    </div>
    <WorkspaceDialog v-model:open="contextOpen" title="对话上下文" wide>
      <div v-if="contextLoading" class="ws-empty" role="status">读取中…</div>
      <p v-else-if="contextError" class="ws-error">{{ contextError }}</p>
      <template v-else>
        <ResetPostCard v-for="post in contextItems" :key="post.id" :post="post" />
        <p v-if="!contextItems.length" class="ws-muted">暂无上下文，可查看原帖。</p>
      </template>
      <template #footer><a v-if="safeExternalURL(contextSource)" class="ws-button" :href="safeExternalURL(contextSource)" target="_blank" rel="noopener noreferrer">查看原帖<Icon name="externalLink" size="sm" /></a></template>
    </WorkspaceDialog>
    </div>
  </WorkspaceShell>
</template>

<style scoped>
.reset-toolbar { display: flex; justify-content: flex-end; align-items: center; gap: 12px; margin-bottom: 16px; }
.reset-toolbar .ws-button { min-height: 30px; padding: 5px 9px; font-size: 11px; background: transparent; }
.reset-more { padding: 12px; text-align: center; }
.original-liquid-glass-ui-filterTab > span { margin-left: 3px; font-size: 10px; opacity: .7; }
</style>
