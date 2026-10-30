<script setup lang="ts">
import { formatTime, type FeedState } from '@/api/workspace'
defineProps<{ state: FeedState | null; loading?: boolean }>()
</script>

<template>
  <span class="feed-status" :class="{ 'is-cached': state?.stale }" role="status">
    <i aria-hidden="true" />
    <template v-if="state?.fetchedAt">{{ state.stale ? '上次同步' : '已更新' }} {{ formatTime(state.fetchedAt) }}</template>
    <template v-else>{{ loading ? '更新中' : '等待更新' }}</template>
    <span v-if="state?.stale" class="cache-label">缓存</span>
  </span>
</template>

<style scoped>
.feed-status { display: inline-flex; align-items: center; flex-wrap: wrap; gap: 7px; color: var(--ws-muted); font-size: 11px; }
.feed-status i { width: 6px; height: 6px; border-radius: 50%; background: #22a57b; }
.feed-status.is-cached i { background: #d49a35; }
.cache-label { padding: 2px 6px; border-radius: 4px; color: #9a681b; background: #e9b4541a; }
:global(.dark) .cache-label { color: #e6b565; }
</style>
