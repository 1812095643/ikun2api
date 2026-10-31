<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import TutorialLink from '@/components/common/TutorialLink.vue'
import CompactHome from '@/components/landing/CompactHome.vue'
import PrismLanding from '@/components/landing/PrismLanding.vue'
import { useLandingTheme } from '@/composables/useLandingTheme'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'IKUN2api')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'AI API Gateway Platform')
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const hasHomeContent = computed(() => homeContent.value.trim().length > 0)
const compactHomeEnabled = computed(() => appStore.cachedPublicSettings?.compact_home_enabled === true)
const isHomeContentUrl = computed(() => /^https?:\/\//.test(homeContent.value.trim()))
const isAuthenticated = computed(() => authStore.isAuthenticated)
const dashboardPath = computed(() => authStore.isAdmin ? '/admin/dashboard' : '/dashboard')
const showModelPlazaEntry = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza) &&
  (isAuthenticated.value || appStore.cachedPublicSettings?.model_plaza_require_auth !== true))
// Native model square remains available through the existing sign-in redirect.
const modelsTo = computed(() => showModelPlazaEntry.value ? '/model-plaza' : '/model-square')
const { isDark, toggleTheme } = useLandingTheme()

onMounted(() => {
  authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) appStore.fetchPublicSettings()
})
</script>

<template>
  <!-- Custom Home Content: Full Page Mode -->
  <div v-if="hasHomeContent" class="min-h-screen">
    <nav aria-label="欢迎页文档" class="flex justify-end border-b border-gray-200 bg-gray-50 px-4 py-3 dark:border-dark-800 dark:bg-dark-950">
      <TutorialLink class="btn btn-secondary" />
    </nav>
    <!-- iframe mode -->
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <!-- HTML mode - SECURITY: homeContent is admin-only setting, XSS risk is acceptable -->
    <div v-else v-html="homeContent"></div>
  </div>

  <CompactHome
    v-else-if="compactHomeEnabled"
    :site-name="siteName" :site-logo="siteLogo" :site-subtitle="siteSubtitle"
    :is-authenticated="isAuthenticated" :dashboard-path="dashboardPath"
    :show-model-plaza-entry="showModelPlazaEntry" :is-dark="isDark" @theme="toggleTheme"
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
