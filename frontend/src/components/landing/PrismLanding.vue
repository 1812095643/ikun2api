<script setup lang="ts">
import { useMenuVisibility } from '@/composables/useMenuVisibility'
import { onMounted, onUnmounted, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import LandingHeader from './LandingHeader.vue'
import LandingFeatures from './LandingFeatures.vue'
import LandingSteps from './LandingSteps.vue'
import ProviderStrip from './ProviderStrip.vue'
import PrismScene from './PrismScene.vue'
import TutorialLink from '@/components/common/TutorialLink.vue'
import Icon from '@/components/icons/Icon.vue'
import { useLandingMotion, type LandingFrame } from '@/composables/useLandingMotion'

defineProps<{
  siteName: string
  siteLogo: string
  entryTo: string
  entryLabel: string
  modelsTo: string
}>()
const { t } = useI18n()
const { menuVisible } = useMenuVisibility()
const root = shallowRef<HTMLElement | null>(null)
const shade = shallowRef<HTMLElement | null>(null)
const revealReady = shallowRef(false)
const currentYear = new Date().getFullYear()
const { explore, onFrame, scrollHintVisible } = useLandingMotion(root)

// 内容章节（能力、接入）在首页中的位置，用于加深背景遮罩；尺寸变化时重新测量，滚动时不读取布局。
let storyStart = Number.POSITIVE_INFINITY
let storyEnd = Number.POSITIVE_INFINITY
let shadeOpacity = -1
let lastFrame: Readonly<LandingFrame> | undefined
let stopShade: (() => void) | undefined
let sizeObserver: ResizeObserver | undefined
let revealObserver: IntersectionObserver | undefined
let revealRoot: HTMLElement | null = null

const smoothstep = (value: number) => {
  const x = Math.min(1, Math.max(0, value))
  return x * x * (3 - 2 * x)
}

function measureStory() {
  storyStart = root.value?.querySelector<HTMLElement>('#home-explore')?.offsetTop ?? Number.POSITIVE_INFINITY
  storyEnd = root.value?.querySelector<HTMLElement>('#home-start')?.offsetTop ?? Number.POSITIVE_INFINITY
}

function updateShade(frame: Readonly<LandingFrame>) {
  lastFrame = frame
  // 镜头推进末段压暗走廊；进入内容章节时再加深以保证文字清晰，结尾章节回落一部分，露出光亮的走廊。
  const view = frame.viewHeight
  const journey = Math.max(0, frame.progress - .65) * .9
  const enter = smoothstep((frame.offset - storyStart + view * .6) / (view * .6))
  const leave = smoothstep((frame.offset - storyEnd + view) / (view * .7))
  const opacity = Math.round((journey + .34 * enter * (1 - leave * .6)) * 1000) / 1000
  if (opacity === shadeOpacity || !shade.value) return
  shadeOpacity = opacity
  // 只改一个独立合成层的不透明度，不触发布局或重绘。
  shade.value.style.opacity = String(opacity)
}

function reveal(target: Element) {
  target.setAttribute('data-revealed', '')
  revealObserver?.unobserve(target)
}

// 键盘焦点进入尚未淡入的区块时立即显示，避免可聚焦内容停留在透明状态。
function revealFocused(event: FocusEvent) {
  const target = event.target instanceof Element ? event.target.closest('[data-reveal]:not([data-revealed])') : null
  if (target) reveal(target)
}

function setupReveal() {
  const element = root.value
  if (!element || typeof IntersectionObserver === 'undefined') return
  if (window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches) return
  // 每个区块只淡入一次，之后立即停止观察；不支持或减少动态效果时内容始终直接显示。
  // 不收缩观察区域：页底元素在最大滚动位置也一定进入视口，不会一直透明。
  revealObserver = new IntersectionObserver(entries => {
    for (const entry of entries) if (entry.isIntersecting) reveal(entry.target)
  }, { threshold: .12 })
  element.querySelectorAll('[data-reveal]').forEach(target => revealObserver?.observe(target))
  revealRoot = element
  revealRoot.addEventListener('focusin', revealFocused)
  revealReady.value = true
}

onMounted(() => {
  stopShade = onFrame(updateShade)
  // ResizeObserver 在布局完成后首次回调，此时读取 offsetTop 不会强制同步布局。
  if (root.value && typeof ResizeObserver !== 'undefined') {
    sizeObserver = new ResizeObserver(() => {
      measureStory()
      if (lastFrame) {
        shadeOpacity = -1
        updateShade(lastFrame)
      }
    })
    sizeObserver.observe(root.value)
  }
  setupReveal()
})

onUnmounted(() => {
  stopShade?.()
  sizeObserver?.disconnect()
  revealObserver?.disconnect()
  revealRoot?.removeEventListener('focusin', revealFocused)
})
</script>

<template>
  <div ref="root" class="prism-landing" :class="{ 'reveal-ready': revealReady }" data-testid="prism-landing">
    <div class="scene-sticky" aria-hidden="true">
      <PrismScene :subscribe="onFrame" />
      <div ref="shade" class="scene-shade"></div>
    </div>

    <section class="landing-screen landing-snap-section" aria-labelledby="landing-title">
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
          <RouterLink v-if="menuVisible('/model-square')" :to="modelsTo" class="explore-link">
            {{ t('home.prism.exploreModels') }}<Icon name="chevronRight" size="sm" aria-hidden="true" />
          </RouterLink>
        </div>
      </main>

      <div class="hero-providers"><ProviderStrip :to="modelsTo" :linked="menuVisible('/model-square')" /></div>
      <Transition name="scroll-hint">
        <button v-if="scrollHintVisible" class="scroll-cue" type="button" aria-controls="home-explore" @click="explore">
          <span class="scroll-cue-content">
            <Icon name="arrowDown" size="lg" aria-hidden="true" />
            <span>{{ t('home.prism.scroll') }}</span>
          </span>
        </button>
      </Transition>
    </section>

    <LandingFeatures class="landing-snap-section" />
    <LandingSteps class="landing-snap-section" />

    <section id="home-start" class="explore-section landing-snap-section" aria-labelledby="explore-title">
      <div class="explore-content">
        <div data-reveal>
          <p class="landing-eyebrow">YOUR NEXT CONNECTION</p>
          <h2 id="explore-title" class="landing-title">{{ t('home.prism.nextTitle') }}</h2>
        </div>
        <div class="next-links" data-reveal style="--reveal-index: 1">
          <RouterLink v-if="menuVisible('/model-square')" :to="modelsTo" class="next-link">
            <span class="next-number" aria-hidden="true">01</span>
            <span class="next-text"><strong>{{ t('home.prism.models') }}</strong><span>{{ t('home.prism.modelsDescription') }}</span></span>
            <Icon name="arrowRight" size="md" aria-hidden="true" />
          </RouterLink>
          <a v-if="menuVisible('/tutorial')" href="/tutorial" target="_blank" rel="noopener noreferrer" class="next-link" :aria-label="t('home.prism.tutorialNewTab')">
            <span class="next-number" aria-hidden="true">02</span>
            <span class="next-text"><strong>{{ t('home.prism.tutorial') }}</strong><span>{{ t('home.prism.tutorialDescription') }}</span></span>
            <Icon name="externalLink" size="md" aria-hidden="true" />
          </a>
        </div>
        <div class="next-start" data-reveal style="--reveal-index: 2">
          <RouterLink :to="entryTo" class="start-button">{{ t('home.prism.start') }}<Icon name="arrowRight" size="sm" aria-hidden="true" /></RouterLink>
        </div>
      </div>
      <footer class="landing-footer">
        <span>© {{ currentYear }} {{ siteName }}</span>
        <TutorialLink class="footer-tutorial" />
      </footer>
    </section>
  </div>
</template>

<style scoped>
@font-face { font-family: 'Prism Sans SC'; font-style: normal; font-weight: 400; font-display: swap; src: url('../../assets/landing/heading-sc-0.woff2') format('woff2'); }
@font-face { font-family: 'Prism Sans SC'; font-style: normal; font-weight: 500; font-display: swap; src: url('../../assets/landing/heading-sc-1.woff2') format('woff2'); }
@font-face { font-family: 'Prism Sans SC'; font-style: normal; font-weight: 600; font-display: swap; src: url('../../assets/landing/heading-sc-2.woff2') format('woff2'); }
@font-face { font-family: 'Prism Sans SC'; font-style: normal; font-weight: 700; font-display: swap; src: url('../../assets/landing/heading-sc-3.woff2') format('woff2'); }
.prism-landing {
  --landing-type-scale: .85;
  --landing-screen-height: 100svh;
  position: relative; isolation: isolate; color-scheme: dark; background: #0b192c; color: #f7f9ff;
  font-family: 'Prism Sans SC', Arial, 'PingFang SC', 'Microsoft YaHei', sans-serif;
  -webkit-font-smoothing: antialiased; overflow: clip;
}
@supports (height: 100dvh) {
  .prism-landing { --landing-screen-height: 100dvh; }
}
:global(html:has(.prism-landing)) {
  scrollbar-width: none;
  /* 四段逐段落位，快速滚动也在下一段停住；离开首页后自动恢复普通滚动。 */
  scroll-snap-type: y mandatory;
  scroll-padding-block: 0;
}
:global(html:has(.prism-landing)::-webkit-scrollbar) { display: none; }
.prism-landing :deep(.landing-snap-section) {
  scroll-snap-align: start;
  scroll-snap-stop: always;
  /* 吸附前就使用真实高度，避免 content-visibility 的估算占位在靠近时改变落点。 */
  content-visibility: visible;
  contain-intrinsic-size: none;
}
.scene-sticky { position: sticky; top: 0; width: 100%; height: var(--landing-screen-height); min-height: 540px; margin-bottom: calc(-1 * max(var(--landing-screen-height), 540px)); overflow: hidden; pointer-events: none; z-index: -1; }
.scene-shade { position: absolute; inset: 0; background: #061426; opacity: 0; will-change: opacity; }
.landing-screen { position: relative; width: 100%; height: var(--landing-screen-height); min-height: 540px; }
.hero-copy { position: absolute; top: 23.9%; left: 50%; width: min(880px, 90%); text-align: center; transform: translateX(-50%); }
.hero-eyebrow { margin: 0; color: #b4dcfa; font-size: calc((clamp(12px, .86vw, 16px)) * var(--landing-type-scale, 1)); line-height: 1.5; letter-spacing: .21em; font-weight: 400; }
.hero-title { display: flex; flex-direction: column; margin: 14px 0 17px; font-size: calc((clamp(44px, 4.9vw, 94px)) * var(--landing-type-scale, 1)); line-height: 1.13; letter-spacing: .012em; font-weight: 600; color: #f5f8ff; text-shadow: 0 3px 18px rgb(5 16 32 / 12%); }
.hero-title > span { display: block; transform: none; }
.hero-title-second { color: #d5e9ff; background: linear-gradient(180deg, #f3faff 8%, #b9dafa 100%); background-clip: text; -webkit-background-clip: text; -webkit-text-fill-color: transparent; }
.hero-description { margin: 0; color: #d8e0ef; font-size: calc((clamp(16px, 1.4vw, 26px)) * var(--landing-type-scale, 1)); line-height: 1.65; font-weight: 400; letter-spacing: .015em; }
.hero-actions { display: flex; align-items: center; justify-content: center; gap: 35px; margin-top: 31px; }
.start-button { min-height: clamp(48px, 3.71vw, 68px); min-width: clamp(152px, 11.36vw, 218px); display: inline-flex; align-items: center; justify-content: center; gap: 17px; padding: clamp(11px, .9vw, 17px) clamp(24px, 1.8vw, 34px); background: #fff9f1; border: 1px solid rgb(255 255 255 / 70%); border-radius: 999px; color: #111c2c; font-size: calc((clamp(14px, 1.077vw, 20px)) * var(--landing-type-scale, 1)); line-height: 1.5; font-weight: 600; text-decoration: none; box-shadow: 0 8px 35px rgb(137 185 255 / 8%); transition: transform 180ms ease, background 180ms ease, box-shadow 180ms ease; }
.start-button :deep(svg) { width: 19px; height: 19px; transition: transform 180ms ease; }
.start-button:hover { transform: translateY(-3px); background: #fff; box-shadow: 0 8px 36px rgb(157 204 255 / 22%); }
.start-button:hover :deep(svg) { transform: translateX(3px); }
.start-button:active { transform: translateY(-1px); }
.explore-link { display: inline-flex; align-items: center; gap: 10px; min-height: 48px; color: #f2f6ff; font-size: calc((clamp(14px, 1.077vw, 20px)) * var(--landing-type-scale, 1)); font-weight: 500; text-decoration: none; transition: color 180ms ease; }
.explore-link:hover { color: #b6dcff; }
.explore-link :deep(svg) { width: 15px; height: 15px; transition: transform 180ms ease; }
.explore-link:hover :deep(svg) { transform: translateX(3px); }
.hero-providers { position: absolute; bottom: 11.9%; left: 0; width: 100%; }
.scroll-cue { position: absolute; bottom: 4.35%; left: 50%; transform: translateX(-50%); display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 3px 15px; color: #c9d5e5; font-size: calc((14px) * var(--landing-type-scale, 1)); font-weight: 400; line-height: 1.5; }
.scroll-cue-content { display: flex; flex-direction: column; align-items: center; gap: inherit; animation: scroll-cue-breathe 3.2s ease-in-out infinite; }
@keyframes scroll-cue-breathe { 0%, 100% { opacity: .45; transform: translateY(0); } 50% { opacity: 1; transform: translateY(4px); } }
.scroll-hint-enter-active { transition: opacity 400ms ease-out; }
.scroll-hint-leave-active { transition: opacity 220ms ease-out; pointer-events: none; }
.scroll-hint-enter-from, .scroll-hint-leave-to { opacity: 0; }
.scroll-cue :deep(svg) { width: 20px; height: 30px; stroke-width: .8; transition: transform 180ms ease; }
.scroll-cue:hover :deep(svg) { transform: translateY(4px); }
.prism-landing :deep(:is(a, button):focus-visible) { outline: 2px solid #b9ddff; outline-offset: 5px; }

/* 各章节共用的标题层级 */
.prism-landing :deep(.landing-eyebrow) { margin-top: 0; margin-bottom: 20px; color: #b1cee8; font-size: calc(12px * var(--landing-type-scale, 1)); line-height: 1.5; letter-spacing: .22em; }
.prism-landing :deep(.landing-title) { margin-top: 0; margin-bottom: 0; color: #f5f8ff; font-size: calc((clamp(32px, 3vw, 52px)) * var(--landing-type-scale, 1)); font-weight: 600; line-height: 1.4; letter-spacing: .02em; text-wrap: balance; }
/* 只设置上下外边距，左右由各章节决定对齐方式 */
.prism-landing :deep(.landing-lead) { max-width: 560px; margin-top: 16px; margin-bottom: 0; color: #c3d2e6; font-size: calc(clamp(17px, 1vw, 21px) * var(--landing-type-scale, 1)); line-height: 1.75; text-wrap: pretty; }

/* 进入视口时的柔和淡入：只用 opacity 与 transform，由合成线程完成 */
.prism-landing.reveal-ready :deep([data-reveal]) { transition: opacity 760ms cubic-bezier(.22, .61, .36, 1), transform 760ms cubic-bezier(.22, .61, .36, 1); transition-delay: calc(var(--reveal-index, 0) * 90ms); }
.prism-landing.reveal-ready :deep([data-reveal]:not([data-revealed])) { opacity: 0; transform: translate3d(0, 24px, 0); }

.explore-section { position: relative; display: flex; align-items: center; justify-content: center; min-height: var(--landing-screen-height); padding: clamp(40px, 7svh, 100px) 30px clamp(76px, 10svh, 135px); }
.explore-content { width: min(860px, 100%); text-align: center; }
.next-links { margin-top: 52px; border-top: 1px solid rgb(202 226 255 / 28%); }
.next-link { display: flex; align-items: center; gap: 24px; padding: 28px 10px; text-align: left; border-bottom: 1px solid rgb(202 226 255 / 28%); text-decoration: none; color: #f2f6ff; transition: background 180ms ease; }
.next-link:hover { background: rgb(220 237 255 / 6%); }
.next-link > :deep(svg) { flex-shrink: 0; transition: transform 180ms ease; }
.next-link:hover > :deep(svg) { transform: translateX(4px); }
.next-number { align-self: flex-start; padding-top: 7px; font-size: calc((12px) * var(--landing-type-scale, 1)); color: #8ea9c7; }
.next-text { display: flex; flex: 1; flex-direction: column; gap: 9px; }
.next-text strong { font-size: calc((23px) * var(--landing-type-scale, 1)); font-weight: 500; }
.next-text > span { color: #c4d1e3; font-size: calc((15px) * var(--landing-type-scale, 1)); line-height: 1.6; text-wrap: pretty; }
.next-start { margin-top: 42px; }
.landing-footer { position: absolute; bottom: 30px; left: 5%; right: 5%; display: flex; align-items: center; justify-content: space-between; gap: 12px; color: #afc0d6; font-size: calc((12px) * var(--landing-type-scale, 1)); }
.footer-tutorial { display: inline-flex; align-items: center; gap: 7px; color: inherit; text-decoration: none; transition: color 180ms ease; }
.footer-tutorial:hover { color: #e6f0fb; }
@media (min-width: 1800px) { .hero-title { margin-top: 28px; } .hero-actions { margin-top: 38px; } }
@media (max-height: 620px) and (min-width: 701px) {
  .hero-copy { top: 22%; }
  .hero-title { font-size: calc((min(5vw, 9.1svh)) * var(--landing-type-scale, 1)); line-height: 1.13; margin: 12px 0 10px; }
  .hero-description { font-size: calc((19px) * var(--landing-type-scale, 1)); }
  .hero-actions { margin-top: 20px; }
  .start-button { min-height: 49px; min-width: 160px; padding: 11px 24px; font-size: calc((16px) * var(--landing-type-scale, 1)); }
  .explore-link { font-size: calc((16px) * var(--landing-type-scale, 1)); }
  .hero-providers { bottom: 10.5%; }
  .scroll-cue { bottom: 2.4%; gap: 5px; font-size: calc((12px) * var(--landing-type-scale, 1)); }
}
@media (max-width: 700px) {
  .scene-sticky, .landing-screen { min-height: 560px; }
  .scene-sticky { margin-bottom: calc(-1 * max(var(--landing-screen-height), 560px)); }
  .hero-copy { top: 26%; width: calc(100% - 36px); }
  .hero-eyebrow { font-size: calc((10px) * var(--landing-type-scale, 1)); letter-spacing: .18em; }
  .hero-title { font-size: calc((clamp(38px, 10.5vw, 62px)) * var(--landing-type-scale, 1)); margin: 18px 0 18px; line-height: 1.22; letter-spacing: -.025em; }
  .hero-description { font-size: calc((14px) * var(--landing-type-scale, 1)); line-height: 1.7; max-width: 330px; margin-inline: auto; }
  .hero-actions { gap: 22px; margin-top: 26px; }
  .start-button { min-width: 142px; min-height: 48px; font-size: calc((14px) * var(--landing-type-scale, 1)); padding: 12px 22px; gap: 12px; }
  .explore-link { font-size: calc((14px) * var(--landing-type-scale, 1)); gap: 6px; }
  .hero-providers { bottom: 12%; }
  .scroll-cue { bottom: 3.5%; font-size: calc((12px) * var(--landing-type-scale, 1)); gap: 5px; }
  .scroll-cue :deep(svg) { height: 24px; }
  .prism-landing :deep(.landing-title) { font-size: calc(30px * var(--landing-type-scale, 1)); }
  .prism-landing :deep(.landing-lead) { font-size: calc(15px * var(--landing-type-scale, 1)); line-height: 1.7; }
  .explore-section { padding: 58px 24px 76px; }
  .next-links { margin-top: 24px; }
  .next-link { gap: 15px; padding: 17px 0; }
  .next-text strong { font-size: calc((19px) * var(--landing-type-scale, 1)); }
  .next-text > span { font-size: calc((13px) * var(--landing-type-scale, 1)); }
  .next-start { margin-top: 26px; }
}
@media (max-width: 960px) {
  .prism-landing :deep(.steps-section .landing-eyebrow) { margin-bottom: 10px; }
  .prism-landing :deep(.steps-section .landing-title) { font-size: 26px; line-height: 1.3; }
  .prism-landing :deep(.steps-section .landing-lead) { margin-top: 8px; font-size: 13px; line-height: 1.5; }
}
@media (prefers-reduced-motion: reduce) {
  :global(html:has(.prism-landing)) { scroll-snap-type: none; }
  .scroll-cue-content { animation: none; }
  .scene-shade { opacity: 0 !important; }
  .explore-section, .prism-landing :deep(:is(.feature-section, .steps-section)) { background: rgb(6 20 38 / 80%); }
  .prism-landing * { transition: none !important; scroll-behavior: auto !important; }
  /* 浏览中途开启减少动态效果时，尚未淡入的内容也立即完整显示 */
  .prism-landing :deep([data-reveal]) { opacity: 1 !important; transform: none !important; transition: none !important; }
  .start-button:hover, .scroll-cue:hover :deep(svg) { transform: none; }
}
</style>
