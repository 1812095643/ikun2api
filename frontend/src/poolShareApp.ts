import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHistory } from 'vue-router'
import PoolShareView from '@/views/public/PoolShareView.vue'
import i18n, { initI18n, loadLocaleMessages } from '@/i18n'
import { useAppStore } from '@/stores/app'
import { updateFavicon } from '@/utils/branding'

/** Never install the console router or restore a login in an embeddable document. */
export async function bootstrapPoolShareApp() {
  const app = createApp(PoolShareView)
  app.use(createPinia())
  const settings = useAppStore()
  settings.initFromInjectedConfig()
  updateFavicon(settings.siteLogo)

  const params = new URLSearchParams(window.location.search)
  const theme = params.get('theme')
  if (theme === 'light' || theme === 'dark') document.documentElement.classList.toggle('dark', theme === 'dark')
  await initI18n()
  const language = params.get('lang')?.toLowerCase().split('-')[0]
  if (language === 'zh' || language === 'en') {
    await loadLocaleMessages(language)
    i18n.global.locale.value = language
    document.documentElement.lang = language
  }

  const router = createRouter({
    history: createWebHistory(),
    routes: [
      { path: '/share/pool', component: PoolShareView },
      { path: '/:pathMatch(.*)*', redirect: '/share/pool' },
    ],
  })
  app.use(router)
  app.use(i18n)
  await router.isReady()
  app.mount('#app')
  return { app, router }
}
