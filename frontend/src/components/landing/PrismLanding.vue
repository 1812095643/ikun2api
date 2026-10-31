<script setup lang="ts">
import { shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import LandingHeader from './LandingHeader.vue'
import ProviderStrip from './ProviderStrip.vue'
import PrismScene from './PrismScene.vue'
import TutorialLink from '@/components/common/TutorialLink.vue'
import Icon from '@/components/icons/Icon.vue'
import { useLandingMotion } from '@/composables/useLandingMotion'

defineProps<{
  siteName: string
  siteLogo: string
  entryTo: string
  entryLabel: string
  modelsTo: string
}>()
const { t } = useI18n()
const root = shallowRef<HTMLElement | null>(null)
const { explore, progress, pointerX, pointerY } = useLandingMotion(root)
</script>

<template>
  <div ref="root" class="prism-landing" data-testid="prism-landing">
    <div class="scene-sticky" aria-hidden="true">
      <PrismScene :progress="progress" :pointer-x="pointerX" :pointer-y="pointerY" />
      <div class="scene-shade"></div>
    </div>

    <section class="landing-screen" aria-labelledby="landing-title">
      <LandingHeader :site-name="siteName" :site-logo="siteLogo" :entry-to="entryTo" :entry-label="entryLabel" :models-to="modelsTo" />

      <main class="hero-copy">
        <p class="hero-eyebrow">ALL MODELS. ONE CONNECTION.</p>
        <h1 id="landing-title" class="hero-title">
          <span>{{ t('home.prism.titleFirst') }}</span>
          <span class="hero-title-second">{{ t('home.prism.titleSecond') }}</span>
        </h1>
        <p class="hero-description">{{ t('home.prism.description') }}</p>
        <div class="hero-actions">
          <RouterLink :to="entryTo" class="start-button" data-testid="landing-start">
            {{ t('home.prism.start') }}<Icon name="arrowRight" size="sm" aria-hidden="true" />
          </RouterLink>
          <RouterLink :to="modelsTo" class="explore-link">
            {{ t('home.prism.exploreModels') }}<Icon name="chevronRight" size="sm" aria-hidden="true" />
          </RouterLink>
        </div>
      </main>

      <div class="hero-providers"><ProviderStrip :to="modelsTo" /></div>
      <button class="scroll-cue" type="button" aria-controls="home-explore" @click="explore">
        <Icon name="arrowDown" size="lg" aria-hidden="true" />
        <span>{{ t('home.prism.scroll') }}</span>
      </button>
    </section>

    <div class="camera-journey" aria-hidden="true"></div>
    <section id="home-explore" class="explore-section" aria-labelledby="explore-title">
      <div class="explore-content">
        <p class="section-eyebrow">YOUR NEXT CONNECTION</p>
        <h2 id="explore-title" class="explore-title">{{ t('home.prism.nextTitle') }}</h2>
        <div class="next-links">
          <RouterLink :to="modelsTo" class="next-link">
            <span class="next-number" aria-hidden="true">01</span>
            <span class="next-text"><strong>{{ t('home.prism.models') }}</strong><span>{{ t('home.prism.modelsDescription') }}</span></span>
            <Icon name="arrowRight" size="md" aria-hidden="true" />
          </RouterLink>
          <a href="/tutorial" target="_blank" rel="noopener noreferrer" class="next-link" :aria-label="t('home.prism.tutorialNewTab')">
            <span class="next-number" aria-hidden="true">02</span>
            <span class="next-text"><strong>{{ t('home.prism.tutorial') }}</strong><span>{{ t('home.prism.tutorialDescription') }}</span></span>
            <Icon name="externalLink" size="md" aria-hidden="true" />
          </a>
        </div>
        <RouterLink :to="entryTo" class="start-button next-start">{{ t('home.prism.start') }}<Icon name="arrowRight" size="sm" aria-hidden="true" /></RouterLink>
      </div>
      <footer class="landing-footer">
        <span>© {{ new Date().getFullYear() }} {{ siteName }}</span>
        <TutorialLink class="footer-tutorial" />
      </footer>
    </section>
  </div>
</template>

<style scoped>
@font-face { font-family: 'Prism Sans SC'; font-style: normal; font-weight: 400; font-display: swap; src: url('../../assets/landing/heading-sc-0.ttf') format('truetype'); }
@font-face { font-family: 'Prism Sans SC'; font-style: normal; font-weight: 500; font-display: swap; src: url('../../assets/landing/heading-sc-1.ttf') format('truetype'); }
@font-face { font-family: 'Prism Sans SC'; font-style: normal; font-weight: 600; font-display: swap; src: url('../../assets/landing/heading-sc-2.ttf') format('truetype'); }
@font-face { font-family: 'Prism Sans SC'; font-style: normal; font-weight: 700; font-display: swap; src: url('../../assets/landing/heading-sc-3.ttf') format('truetype'); }
.prism-landing {
  --journey: 0; --pointer-x: 0; --pointer-y: 0; --overscan: 0;
  position: relative; isolation: isolate; color-scheme: dark; background: #0b192c; color: #f7f9ff;
  font-family: 'Prism Sans SC', Arial, 'PingFang SC', 'Microsoft YaHei', sans-serif;
  -webkit-font-smoothing: antialiased; overflow: clip;
}
:global(html:has(.prism-landing)) { scrollbar-width: none; }
:global(html:has(.prism-landing)::-webkit-scrollbar) { display: none; }
.scene-sticky { position: sticky; top: 0; width: 100%; height: 100svh; min-height: 540px; margin-bottom: calc(-1 * max(100svh, 540px)); overflow: hidden; pointer-events: none; z-index: -1; }
.scene-shade { position: absolute; inset: 0; background: #061426; opacity: calc(max(0, var(--journey) - .65) * .9); }
.camera-journey { height: 80svh; pointer-events: none; }
.landing-screen { position: relative; width: 100%; height: 100svh; min-height: 540px; }
.hero-copy { position: absolute; top: 23.9%; left: 50%; width: min(880px, 90%); text-align: center; transform: translateX(-50%); }
.hero-eyebrow { margin: 0; color: #b4dcfa; font-size: clamp(12px, .86vw, 16px); line-height: 1.5; letter-spacing: .21em; font-weight: 400; }
.hero-title { display: flex; flex-direction: column; margin: 14px 0 17px; font-size: clamp(44px, 4.9vw, 94px); line-height: 1.13; letter-spacing: .012em; font-weight: 600; color: #f5f8ff; text-shadow: 0 3px 18px rgb(5 16 32 / 12%); }
.hero-title > span { display: block; transform: none; }
.hero-title-second { color: #d5e9ff; background: linear-gradient(180deg, #f3faff 8%, #b9dafa 100%); background-clip: text; -webkit-background-clip: text; -webkit-text-fill-color: transparent; }
.hero-description { margin: 0; color: #d8e0ef; font-size: clamp(16px, 1.4vw, 26px); line-height: 1.65; font-weight: 400; letter-spacing: .015em; }
.hero-actions { display: flex; align-items: center; justify-content: center; gap: 35px; margin-top: 31px; }
.start-button { min-height: clamp(48px, 3.71vw, 68px); min-width: clamp(152px, 11.36vw, 218px); display: inline-flex; align-items: center; justify-content: center; gap: 17px; padding: clamp(11px, .9vw, 17px) clamp(24px, 1.8vw, 34px); background: #fff9f1; border: 1px solid rgb(255 255 255 / 70%); border-radius: 999px; color: #111c2c; font-size: clamp(14px, 1.077vw, 20px); line-height: 1.5; font-weight: 600; text-decoration: none; box-shadow: 0 8px 35px rgb(137 185 255 / 8%); transition: transform 180ms ease, background 180ms ease, box-shadow 180ms ease; }
.start-button :deep(svg) { width: 19px; height: 19px; }
.start-button:hover { transform: translateY(-3px); background: #fff; box-shadow: 0 8px 36px rgb(157 204 255 / 22%); }
.explore-link { display: inline-flex; align-items: center; gap: 10px; min-height: 48px; color: #f2f6ff; font-size: clamp(14px, 1.077vw, 20px); font-weight: 500; text-decoration: none; transition: color 180ms ease; }
.explore-link:hover { color: #b6dcff; }
.explore-link :deep(svg) { width: 15px; height: 15px; }
.hero-providers { position: absolute; bottom: 11.9%; left: 0; width: 100%; }
.scroll-cue { position: absolute; bottom: 4.35%; left: 50%; transform: translateX(-50%); display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 3px 15px; color: #c9d5e5; font-size: 14px; font-weight: 400; line-height: 1.5; }
.scroll-cue :deep(svg) { width: 20px; height: 30px; stroke-width: .8; transition: transform 180ms ease; }
.scroll-cue:hover :deep(svg) { transform: translateY(4px); }
.prism-landing :is(a, button):focus-visible { outline: 2px solid #b9ddff; outline-offset: 5px; }
.explore-section { position: relative; display: flex; align-items: center; justify-content: center; min-height: 100svh; padding: 100px 30px 135px; }
.explore-content { width: min(860px, 100%); text-align: center; }
.section-eyebrow { font-size: 12px; letter-spacing: .22em; color: #b1cee8; margin: 0 0 22px; }
.explore-title { margin: 0; font-size: clamp(32px, 3vw, 52px); font-weight: 600; line-height: 1.4; letter-spacing: .02em; }
.next-links { margin-top: 52px; border-top: 1px solid rgb(202 226 255 / 28%); }
.next-link { display: flex; align-items: center; gap: 24px; padding: 28px 10px; text-align: left; border-bottom: 1px solid rgb(202 226 255 / 28%); text-decoration: none; color: #f2f6ff; transition: background 180ms ease; }
.next-link:hover { background: rgb(220 237 255 / 6%); }
.next-number { align-self: flex-start; padding-top: 7px; font-size: 12px; color: #8ea9c7; }
.next-text { display: flex; flex: 1; flex-direction: column; gap: 9px; }
.next-text strong { font-size: 23px; font-weight: 500; }
.next-text > span { color: #becde1; font-size: 15px; line-height: 1.6; }
.next-start { margin-top: 42px; }
.landing-footer { position: absolute; bottom: 30px; left: 5%; right: 5%; display: flex; align-items: center; justify-content: space-between; gap: 12px; color: #afc0d6; font-size: 12px; }
.footer-tutorial { display: inline-flex; align-items: center; gap: 7px; }
@media (min-width: 1800px) { .hero-title { margin-top: 28px; } .hero-actions { margin-top: 38px; } }
@media (max-height: 620px) and (min-width: 701px) {
  .hero-copy { top: 22%; }
  .hero-title { font-size: min(5vw, 9.1svh); line-height: 1.13; margin: 12px 0 10px; }
  .hero-description { font-size: 19px; }
  .hero-actions { margin-top: 20px; }
  .start-button { min-height: 49px; min-width: 160px; padding: 11px 24px; font-size: 16px; }
  .explore-link { font-size: 16px; }
  .hero-providers { bottom: 10.5%; }
  .scroll-cue { bottom: 2.4%; gap: 5px; font-size: 12px; }
}
@media (max-width: 700px) {
  .scene-sticky, .landing-screen { min-height: 650px; }
  .scene-sticky { margin-bottom: calc(-1 * max(100svh, 650px)); }
  .scene-art { object-position: 50% center; }
  .hero-copy { top: 26%; width: calc(100% - 36px); }
  .hero-eyebrow { font-size: 10px; letter-spacing: .18em; }
  .hero-title { font-size: clamp(38px, 10.5vw, 62px); margin: 18px 0 18px; line-height: 1.22; letter-spacing: -.025em; }
  .hero-description { font-size: 14px; line-height: 1.7; max-width: 330px; margin-inline: auto; }
  .hero-actions { gap: 22px; margin-top: 26px; }
  .start-button { min-width: 142px; min-height: 48px; font-size: 14px; padding: 12px 22px; gap: 12px; }
  .explore-link { font-size: 14px; gap: 6px; }
  .hero-providers { bottom: 12%; }
  .scroll-cue { bottom: 3.5%; font-size: 12px; gap: 5px; }
  .scroll-cue :deep(svg) { height: 24px; }
  .explore-section { min-height: 100svh; padding: 80px 24px 100px; }
  .next-links { margin-top: 32px; }
  .next-link { gap: 15px; padding: 24px 0; }
  .next-text strong { font-size: 19px; }
  .next-text > span { font-size: 13px; }
}
@media (prefers-reduced-motion: reduce) {
  .camera-journey { height: 0; }
  .scene-shade { opacity: 0; }
  .explore-section { background: rgb(6 20 38 / 80%); }
  .prism-landing * { transition: none !important; scroll-behavior: auto !important; }
  .start-button:hover, .scroll-cue:hover :deep(svg) { transform: none; }
}
</style>
