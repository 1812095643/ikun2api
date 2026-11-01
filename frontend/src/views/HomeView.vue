<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import TutorialLink from '@/components/common/TutorialLink.vue'
import CompactHome from '@/components/landing/CompactHome.vue'
import LegacyHome from '@/components/landing/LegacyHome.vue'
import PrismLanding from '@/components/landing/PrismLanding.vue'
import { useLandingTheme } from '@/composables/useLandingTheme'
import { sanitizeUrl } from '@/utils/url'
import { useMenuVisibility } from '@/composables/useMenuVisibility'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'IKUN2api')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'AI API Gateway Platform')
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const compactHomeEnabled = computed(() => appStore.cachedPublicSettings?.compact_home_enabled === true)
const homeMode = computed(() => {
  const configured = String(appStore.cachedPublicSettings?.home_mode || '').trim().toLowerCase()
  if (configured === 'prism' || configured === 'legacy' || configured === 'compact' || configured === 'custom') return configured
  if (homeContent.value.trim()) return 'custom'
  if (compactHomeEnabled.value) return 'compact'
  return 'prism'
})
const isHomeContentUrl = computed(() => /^https?:\/\//.test(homeContent.value.trim()))
const isAuthenticated = computed(() => authStore.isAuthenticated)
const dashboardPath = computed(() => authStore.isAdmin ? '/admin/dashboard' : '/dashboard')
const userInitial = computed(() => authStore.user?.email?.charAt(0).toUpperCase() || '')
const { menuVisible } = useMenuVisibility()
const showModelPlazaEntry = computed(() => menuVisible('/model-square'))
const showTutorialEntry = computed(() => menuVisible('/tutorial'))
const modelsTo = '/model-square'
const { isDark, toggleTheme } = useLandingTheme()

onMounted(() => {
  authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) appStore.fetchPublicSettings()
})
</script>

<template>
  <div v-if="homeMode === 'custom'" class="min-h-screen">
    <nav aria-label="欢迎页文档" class="flex justify-end border-b border-gray-200 bg-gray-50 px-4 py-3 dark:border-dark-800 dark:bg-dark-950">
      <TutorialLink class="btn btn-secondary" />
    </nav>
    <iframe v-if="isHomeContentUrl" :src="homeContent.trim()" class="h-screen w-full border-0" allowfullscreen></iframe>
    <div v-else-if="homeContent.trim()" v-html="homeContent"></div>
    <PrismLanding v-else :site-name="siteName" :site-logo="siteLogo" :entry-to="isAuthenticated ? dashboardPath : '/login'" :entry-label="isAuthenticated ? t('home.dashboard') : t('home.login')" :models-to="modelsTo" />
  </div>

  <CompactHome
    v-else-if="homeMode === 'compact'"
    :site-name="siteName" :site-logo="siteLogo" :site-subtitle="siteSubtitle"
    :is-authenticated="isAuthenticated" :dashboard-path="dashboardPath"
    :show-model-plaza-entry="showModelPlazaEntry" :show-tutorial-entry="showTutorialEntry" :is-dark="isDark" @theme="toggleTheme"
  />

  <LegacyHome
    v-else-if="homeMode === 'legacy'"
    :site-name="siteName" :site-logo="siteLogo" :site-subtitle="siteSubtitle" :doc-url="docUrl"
    :is-authenticated="isAuthenticated" :dashboard-path="dashboardPath" :show-model-plaza-entry="showModelPlazaEntry"
    :is-dark="isDark" :user-initial="userInitial" @theme="toggleTheme"
  />

  <PrismLanding
    v-else
    :site-name="siteName"
    :site-logo="siteLogo"
    :entry-to="isAuthenticated ? dashboardPath : '/login'"
    :entry-label="isAuthenticated ? t('home.dashboard') : t('home.login')"
    :models-to="modelsTo"
  />
</template>
