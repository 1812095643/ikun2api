<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import { useEventListener } from '@vueuse/core'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { sanitizeUrl } from '@/utils/url'

const appStore = useAppStore()
const authStore = useAuthStore()
const isDark = shallowRef(document.documentElement.classList.contains('dark'))
const siteName = computed(() => appStore.siteName || 'IKUN2api')
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '/logo.svg', { allowRelative: true, allowDataUrl: true }))
const consolePath = computed(() => !authStore.isAuthenticated ? '/login' : authStore.isAdmin ? '/admin/dashboard' : '/dashboard')

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

useEventListener(window, 'storage', event => {
  if (event.key !== 'theme') return
  isDark.value = event.newValue === 'dark' || (!event.newValue && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.classList.toggle('dark', isDark.value)
})
</script>

<template>
  <header class="tutorial-header">
    <div class="tutorial-header-inner">
      <RouterLink to="/tutorial" class="tutorial-brand" aria-label="新手教程首页">
        <img :src="siteLogo" alt="" width="30" height="30">
        <span>{{ siteName }}</span>
        <span class="tutorial-brand-label">使用文档</span>
      </RouterLink>
      <nav class="tutorial-header-actions" aria-label="文档导航">
        <button type="button" class="tutorial-theme-button" :aria-label="isDark ? '切换浅色主题' : '切换深色主题'" @click="toggleTheme">
          <Icon :name="isDark ? 'sun' : 'moon'" size="md" />
        </button>
        <a :href="consolePath" target="_blank" rel="noopener noreferrer" class="tutorial-console-link">
          {{ authStore.isAuthenticated ? '进入控制台' : '登录网站' }}
          <Icon name="externalLink" size="sm" />
        </a>
      </nav>
    </div>
  </header>
</template>
