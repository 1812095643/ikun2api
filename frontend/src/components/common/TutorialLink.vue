<script setup lang="ts">
import { useMenuLabels } from '@/composables/useMenuLabels'
import { computed } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import SidebarBadge from '@/components/layout/SidebarBadge.vue'
import type { NavigationBadge } from '@/api/navigation'
import { useMenuVisibility } from '@/composables/useMenuVisibility'
const { menuVisible } = useMenuVisibility()
const { menuLabel } = useMenuLabels()
const label = computed(() => menuLabel('/tutorial', '新手教程'))

defineProps<{ compact?: boolean; badge?: NavigationBadge }>()
</script>

<template>
  <a v-if="menuVisible('/tutorial')" href="/tutorial" target="_blank" rel="noopener noreferrer" :title="label + '（新标签页打开）'" :aria-label="label + (badge?.dot || badge?.count ? '，' + badge.label : '') + '（新标签页打开）'">
    <Icon name="book" size="sm" />
    <span :class="{ 'hidden sm:inline': compact }">{{ label }}</span>
    <SidebarBadge v-if="badge" :badge="badge" />
  </a>
</template>
