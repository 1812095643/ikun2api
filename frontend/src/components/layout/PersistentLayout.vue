<script setup lang="ts">
import '@/styles/onboarding.css'
import { computed, provide } from 'vue'
import { useAppStore } from '@/stores'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'
import { LAYOUT_SHELL, createLayoutShell } from './layoutShell'

const appStore = useAppStore()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const shell = createLayoutShell()
provide(LAYOUT_SHELL, shell)
</script>

<template>
  <!--
    RouterView 始终挂在同一位置：没有页面使用 AppLayout 时三层容器都是 display: contents，
    等同于直接渲染 RouterView；切到后台页面时侧边栏和顶栏常驻，切换菜单只替换中间内容。
    结构与 AppLayout 原布局一致（.native-workspace > .pointer-events-none 等样式依赖此层级）。
  -->
  <div
    :class="shell.active ? ['min-h-screen bg-gray-50 dark:bg-dark-950', shell.rootClass] : 'contents'"
    :style="shell.active ? shell.rootStyle : undefined"
  >
    <template v-if="shell.active">
      <!-- Background Decoration -->
      <div class="pointer-events-none fixed inset-0 bg-mesh-gradient"></div>

      <!-- Sidebar -->
      <AppSidebar />
    </template>

    <!-- Main Content Area -->
    <div
      :class="
        shell.active
          ? ['relative min-h-screen transition-all duration-300', sidebarCollapsed ? 'lg:ml-[72px]' : 'lg:ml-64']
          : 'contents'
      "
    >
      <!-- Header -->
      <AppHeader v-if="shell.active" />

      <!-- Main Content -->
      <div
        :class="shell.active ? 'p-4 md:p-6 lg:p-8' : 'contents'"
        :role="shell.active ? 'main' : undefined"
        :data-tour="shell.active ? 'user-page-content' : undefined"
      >
        <slot />
      </div>
    </div>
  </div>
</template>
