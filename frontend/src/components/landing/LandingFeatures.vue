<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
// 四项能力均对应站内已有功能：多平台分组、协议兼容、账号池调度、使用记录与密钥限制。
const features = computed(() => [
  { icon: 'cube', title: t('home.prism.features.platformTitle'), description: t('home.prism.features.platformDescription') },
  { icon: 'terminal', title: t('home.prism.features.protocolTitle'), description: t('home.prism.features.protocolDescription') },
  { icon: 'sync', title: t('home.prism.features.routingTitle'), description: t('home.prism.features.routingDescription') },
  { icon: 'chartBar', title: t('home.prism.features.usageTitle'), description: t('home.prism.features.usageDescription') }
] as const)
</script>

<template>
  <section id="home-explore" class="feature-section" aria-labelledby="features-title">
    <div class="feature-inner">
      <header class="feature-heading" data-reveal>
        <p class="landing-eyebrow">SIMPLE BY DESIGN</p>
        <h2 id="features-title" class="landing-title">{{ t('home.prism.features.title') }}</h2>
        <p class="landing-lead">{{ t('home.prism.features.description') }}</p>
      </header>
      <ul class="feature-grid">
        <li v-for="(feature, index) in features" :key="feature.icon" class="feature-item" data-reveal :style="{ '--reveal-index': index + 1 }">
          <span class="feature-icon"><Icon :name="feature.icon" size="md" aria-hidden="true" /></span>
          <div class="feature-text">
            <h3>{{ feature.title }}</h3>
            <p>{{ feature.description }}</p>
          </div>
        </li>
      </ul>
    </div>
  </section>
</template>

<style scoped>
/* 章节按真实内容布局，吸附位置不受屏幕外估算高度影响。 */
.feature-section { position: relative; display: flex; align-items: center; min-height: var(--landing-screen-height, 100svh); padding: clamp(32px, 6svh, 80px) 6vw; }
/* 1600 宽及以下保持 1180px，超宽屏随视口放大，避免内容显得过小 */
.feature-inner { width: min(clamp(1180px, 70vw, 1440px), 100%); margin: 0 auto; }
.feature-heading { text-align: center; }
.feature-heading .landing-lead { margin-inline: auto; }
.feature-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 48px clamp(28px, 3vw, 48px); margin: clamp(56px, 7svh, 84px) 0 0; padding: 0; list-style: none; }
.feature-item { position: relative; display: flex; flex-direction: column; padding-top: 32px; border-top: 1px solid rgb(202 226 255 / 20%); }
/* 分隔线左端的短高光，呼应门框玻璃的反光 */
.feature-item::before { content: ''; position: absolute; top: -1px; left: 0; width: 56px; height: 1px; background: linear-gradient(90deg, #d7ecff, rgb(215 236 255 / 0%)); }
.feature-icon { display: grid; place-items: center; width: 44px; height: 44px; border: 1px solid rgb(206 229 255 / 24%); border-radius: 50%; color: #d8ecff; background: radial-gradient(circle at 32% 26%, rgb(214 236 255 / 16%), rgb(214 236 255 / 3%) 72%); }
.feature-icon :deep(svg) { width: 20px; height: 20px; }
.feature-text h3 { margin: 26px 0 10px; color: #f3f7ff; font-size: calc(clamp(21px, 1.2vw, 26px) * var(--landing-type-scale, 1)); font-weight: 500; line-height: 1.45; letter-spacing: .01em; }
/* pretty 避免末行只剩一两个字 */
.feature-text p { margin: 0; color: #c4d1e3; font-size: calc(clamp(16px, .95vw, 19px) * var(--landing-type-scale, 1)); line-height: 1.8; text-wrap: pretty; }
@media (max-width: 1100px) {
  .feature-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 24px 32px; margin-top: 32px; }
  .feature-item { padding-top: 20px; }
  .feature-text h3 { margin-top: 16px; }
}
@media (max-width: 700px) {
  .feature-section { padding: 30px 22px 34px; }
  .feature-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 18px; margin-top: 18px; }
  .feature-item { display: block; padding: 12px 0 14px; }
  .feature-item::before { width: 40px; }
  .feature-icon { width: 32px; height: 32px; }
  .feature-icon :deep(svg) { width: 15px; height: 15px; }
  .feature-text h3 { margin: 10px 0 6px; font-size: 15px; line-height: 1.4; }
  .feature-text p { font-size: 13px; line-height: 1.55; }
}
</style>
