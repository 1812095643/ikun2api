<script setup lang="ts">
import { computed } from 'vue'
import type { NavigationBadge } from '@/api/navigation'

const props = defineProps<{ badge: NavigationBadge; collapsed?: boolean }>()
const text = computed(() => props.badge.count > 99 ? '99+' : String(props.badge.count))
</script>

<template>
  <span v-if="badge.count > 0 || badge.dot" class="sidebar-notice" :class="{ 'sidebar-notice-collapsed': collapsed }" :aria-label="badge.label" :title="badge.label">
    <span v-if="badge.count > 0" class="sidebar-notice-count" aria-hidden="true">{{ text }}</span>
    <span v-else class="sidebar-notice-dot" aria-hidden="true"></span>
  </span>
</template>

<style scoped>
.sidebar-notice { display: inline-flex; align-items: center; justify-content: center; flex-shrink: 0; margin-left: auto; }
.sidebar-notice-count { min-width: 18px; height: 18px; display: inline-flex; justify-content: center; align-items: center; padding: 0 5px; border-radius: 10px; background: #dc2626; color: #fff; font-size: 10px; font-weight: 650; line-height: 1; font-variant-numeric: tabular-nums; }
.sidebar-notice-dot { display: block; width: 7px; height: 7px; border-radius: 50%; background: #ef4444; }
.sidebar-notice-collapsed { position: absolute; top: 4px; right: 4px; margin: 0; }
.sidebar-notice-collapsed .sidebar-notice-count { min-width: 15px; height: 15px; padding: 0 3px; font-size: 9px; }
</style>
