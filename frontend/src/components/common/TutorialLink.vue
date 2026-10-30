<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import SidebarBadge from '@/components/layout/SidebarBadge.vue'
import type { NavigationBadge } from '@/api/navigation'

defineProps<{ compact?: boolean; badge?: NavigationBadge }>()
</script>

<template>
  <a href="/tutorial" target="_blank" rel="noopener noreferrer" title="新手教程（新标签页打开）" :aria-label="badge?.dot || badge?.count ? `新手教程，${badge.label}（新标签页打开）` : '新手教程（新标签页打开）'">
    <Icon name="book" size="sm" />
    <span :class="{ 'hidden sm:inline': compact }">新手教程</span>
    <SidebarBadge v-if="badge" :badge="badge" />
  </a>
</template>
