<script setup lang="ts">
import TutorialLink from '@/components/common/TutorialLink.vue'
import Icon from '@/components/icons/Icon.vue'
import { useI18n } from 'vue-i18n'
import { onMounted, onUnmounted, shallowRef } from 'vue'

defineProps<{
  siteName: string
  siteLogo: string
  entryTo: string
  entryLabel: string
  modelsTo: string
}>()
const { t } = useI18n()
const menuOpen = shallowRef(false)
const menu = shallowRef<HTMLElement | null>(null)
function closeOutside(event: PointerEvent) {
  if (menuOpen.value && !menu.value?.contains(event.target as Node)) menuOpen.value = false
}
onMounted(() => document.addEventListener('pointerdown', closeOutside))
onUnmounted(() => document.removeEventListener('pointerdown', closeOutside))
</script>

<template>
  <header class="landing-header">
    <RouterLink to="/home" class="landing-brand" :aria-label="siteName">
      <img class="brand-logo" :src="siteLogo || '/logo.svg'" alt="" width="40" height="40" />
      <span class="brand-name">{{ siteName }}</span>
    </RouterLink>
    <nav class="landing-nav" :aria-label="t('home.prism.navigation')">
      <RouterLink :to="modelsTo" class="nav-link model-link">{{ t('home.prism.models') }}</RouterLink>
      <TutorialLink class="nav-link tutorial-link" />
      <RouterLink :to="entryTo" class="login-link" data-testid="landing-login">{{ entryLabel }}</RouterLink>
      <div ref="menu" class="mobile-menu" @keydown.esc="menuOpen = false">
        <button type="button" class="menu-button" :aria-label="t('home.prism.menu')" :aria-expanded="menuOpen" aria-controls="landing-mobile-links" @click="menuOpen = !menuOpen">
          <Icon :name="menuOpen ? 'x' : 'menu'" size="md" aria-hidden="true" />
        </button>
        <div v-if="menuOpen" id="landing-mobile-links" class="mobile-links" @click="menuOpen = false">
          <RouterLink :to="modelsTo">{{ t('home.prism.models') }}<Icon name="chevronRight" size="sm" aria-hidden="true" /></RouterLink>
          <TutorialLink />
        </div>
      </div>
    </nav>
  </header>
</template>

<style scoped>
.landing-header { position: absolute; z-index: 3; top: 0; left: 0; width: 100%; display: flex; align-items: center; justify-content: space-between; gap: 24px; padding: clamp(22px, 3.2svh, 36px) 4.85%; border-bottom: 1px solid rgb(215 234 255 / 10%); background: rgb(15 30 50 / 8%); backdrop-filter: blur(8px) saturate(120%); -webkit-backdrop-filter: blur(8px) saturate(120%); box-shadow: 0 4px 24px rgb(0 8 24 / 4%), inset 0 1px 0 rgb(255 255 255 / 5%); }
.landing-brand { display: inline-flex; align-items: center; gap: 12px; min-width: 0; max-width: 48%; font-size: clamp(24px, 1.92vw, 36px); line-height: 1.2; font-weight: 600; letter-spacing: -.4px; color: #f7f7fa; text-decoration: none; }
.brand-logo { width: 40px; height: 40px; flex-shrink: 0; border-radius: 9px; object-fit: contain; }
.brand-name { min-width: 0; overflow-wrap: anywhere; }
.landing-nav { display: flex; align-items: center; gap: clamp(22px, 2.55vw, 46px); font-size: clamp(14px, .96vw, 18px); line-height: 1.4; font-weight: 500; }
.nav-link { color: #f5f6fa; white-space: nowrap; text-decoration: none; padding-block: 8px; transition: color 160ms ease; }
.nav-link:hover { color: #adceff; }
.tutorial-link :deep(svg) { display: none; }
.login-link { color: #f9f9fc; border: 1px solid #bac1cb; border-radius: 999px; min-width: 94px; min-height: 42px; display: inline-flex; justify-content: center; align-items: center; padding: 8px 24px; background: rgb(4 12 23 / 32%); text-decoration: none; white-space: nowrap; transition: background 160ms ease; }
.login-link:hover { background: rgb(217 233 255 / 14%); }
.landing-header :is(a, button):focus-visible { outline: 2px solid #b7d9ff; outline-offset: 5px; }
.mobile-menu { display: none; }
@media (max-width: 700px) {
  .landing-header { padding: 18px; gap: 12px; flex-wrap: nowrap; }
  .landing-brand { font-size: 21px; max-width: none; flex: 1; gap: 9px; }
  .brand-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .brand-logo { width: 32px; height: 32px; border-radius: 7px; }
  .landing-nav { gap: 8px; font-size: 13px; flex-shrink: 0; }
  .login-link { min-width: 64px; min-height: 38px; padding: 7px 13px; }
  .model-link, .tutorial-link { display: none; }
  .mobile-menu { display: block; position: relative; }
  .menu-button { width: 38px; height: 38px; display: grid; place-items: center; border: 1px solid rgb(221 237 255 / 30%); border-radius: 50%; background: rgb(11 28 48 / 16%); color: #fff; }
  .mobile-links { position: absolute; top: 50px; right: 0; width: 188px; padding: 8px; border: 1px solid rgb(230 242 255 / 24%); border-radius: 16px; background: rgb(20 40 64 / 76%); backdrop-filter: blur(20px) saturate(140%); box-shadow: 0 14px 40px rgb(0 9 25 / 18%); }
  .mobile-links :deep(a) { display: flex; align-items: center; justify-content: space-between; gap: 10px; min-height: 45px; padding: 10px 13px; color: #f6f9ff; font-size: 14px; border-radius: 10px; }
  .mobile-links :deep(a:hover) { background: rgb(225 241 255 / 12%); }
  .mobile-links :deep(a:last-child svg) { display: none; }
}
@media (prefers-reduced-motion: reduce) { .landing-header * { transition: none; } }
</style>
