<template>
  <slot v-if="shell" />
  <div v-else v-bind="$attrs" class="min-h-screen bg-gray-50 dark:bg-dark-950">
    <!-- Background Decoration -->
    <div class="pointer-events-none fixed inset-0 bg-mesh-gradient"></div>

    <!-- Sidebar -->
    <AppSidebar />

    <!-- Main Content Area -->
    <div
      class="relative min-h-screen transition-all duration-300"
      :class="[sidebarCollapsed ? 'lg:ml-[72px]' : 'lg:ml-64']"
    >
      <!-- Header -->
      <AppHeader />

      <!-- Main Content -->
      <main class="p-4 md:p-6 lg:p-8" data-tour="user-page-content">
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import { computed, inject, onBeforeUpdate, onUnmounted, useAttrs } from 'vue'
import { useAppStore } from '@/stores'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'
import { LAYOUT_SHELL, type LayoutAttrs } from './layoutShell'

defineOptions({ inheritAttrs: false })

const appStore = useAppStore()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)

// 页面传给布局根节点的 class/style（如 WorkspaceShell 的 native-workspace）交给常驻外框的根节点
// 外层有常驻外框（App.vue 的 PersistentLayout）时只渲染 slot，侧边栏和顶栏由外框常驻
const attrs = useAttrs()
const shell = inject(LAYOUT_SHELL, null)
const layoutAttrs = (): LayoutAttrs => ({ class: attrs.class, style: attrs.style as LayoutAttrs['style'] })
if (shell) {
  // setup 阶段登记：同一次渲染里旧页面注销、新页面登记，外框不会被卸载
  const id = shell.register(layoutAttrs())
  onBeforeUpdate(() => shell.update(id, layoutAttrs()))
  onUnmounted(() => shell.unregister(id))
}
</script>
