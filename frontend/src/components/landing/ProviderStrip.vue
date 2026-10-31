<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import claude from '@/assets/landing/claude.png'
import openai from '@/assets/landing/openai.png'
import gemini from '@/assets/landing/gemini.png'
import deepseek from '@/assets/landing/deepseek.png'
import glm from '@/assets/landing/glm.png'
import qwen from '@/assets/landing/qwen.png'

defineProps<{ to: string }>()
const { t } = useI18n()
const providers = [
  { name: 'Claude', image: claude }, { name: 'GPT', image: openai },
  { name: 'Gemini', image: gemini }, { name: 'DeepSeek', image: deepseek },
  { name: 'GLM', image: glm }, { name: 'Qwen', image: qwen }
]
</script>

<template>
  <nav class="provider-strip" :aria-label="t('home.prism.models')">
    <RouterLink v-for="provider in providers" :key="provider.name" :to="to" class="provider-link">
      <img :src="provider.image" :alt="`${provider.name} logo`" width="38" height="38" />
      <span>{{ provider.name }}</span>
    </RouterLink>
    <RouterLink :to="to" class="provider-link provider-more">{{ t('home.prism.more') }}</RouterLink>
  </nav>
</template>

<style scoped>
.provider-strip { font-family: Arial, 'Prism Sans SC', sans-serif; display: flex; align-items: center; justify-content: center; width: max-content; max-width: calc(100% - 40px); min-height: 66px; padding: 10px 8px; margin: 0 auto; border: 0; border-radius: 0; background: rgb(12 24 40 / 12%); box-shadow: none; backdrop-filter: blur(2px); -webkit-backdrop-filter: blur(2px); }
.provider-link { display: inline-flex; position: relative; align-items: center; justify-content: center; gap: 11px; padding: 0 18px; min-height: 44px; white-space: nowrap; font-size: 18px; line-height: 1.3; font-weight: 400; color: #f7f8fc; text-shadow: 0 1px 3px rgb(0 10 28 / 30%); text-decoration: none; transition: color 160ms ease, transform 160ms ease; }
.provider-link + .provider-link::before { content: ''; position: absolute; top: 50%; left: 0; width: 1px; height: 19px; transform: translateY(-50%); background: #8c98ac; opacity: .7; }
.provider-link img { width: 36px; height: 36px; object-fit: contain; flex-shrink: 0; filter: drop-shadow(0 1px 5px rgb(65 116 255 / 16%)); }
.provider-link:hover { color: #c5e4ff; transform: translateY(-2px); }
.provider-link:focus-visible { outline: 2px solid #b7d9ff; outline-offset: 1px; border-radius: 5px; }
.provider-more { padding-inline: 23px 18px; }
@media (max-width: 1100px) {
  .provider-link { font-size: 15px; gap: 9px; padding-inline: 14px; }
  .provider-link img { width: 31px; height: 31px; }
}
@media (max-width: 700px) {
  .provider-strip { display: grid; width: min(380px, calc(100% - 32px)); grid-template-columns: repeat(3, minmax(0, 1fr)); padding: 12px 6px; gap: 8px 0; }
  .provider-link { min-height: 36px; padding: 4px 5px; font-size: 13px; gap: 7px; }
  .provider-link img { width: 27px; height: 27px; }
  .provider-link:nth-child(3n + 1)::before, .provider-more::before { display: none; }
  .provider-more { grid-column: 1 / -1; min-height: 22px; font-size: 12px; padding: 0; }
}
@media (prefers-reduced-motion: reduce) { .provider-link { transition: none; } .provider-link:hover { transform: none; } }
</style>
