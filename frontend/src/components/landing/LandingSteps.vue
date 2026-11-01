<script setup lang="ts">
import { computed, onUnmounted, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { useMenuVisibility } from '@/composables/useMenuVisibility'
import { resolveApiBase } from '@/utils/url'

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()
const { menuVisible } = useMenuVisibility()
// 地址随当前安装变化：优先使用后台配置的 API 地址，否则使用当前站点来源，与教程一致。
const apiBase = computed(() => resolveApiBase(appStore.apiBaseUrl, window.location.origin))
const endpoints = computed(() => [
  { id: 'anthropic', name: 'Claude Code · Anthropic', url: apiBase.value },
  { id: 'openai', name: 'Codex · OpenAI SDK', url: `${apiBase.value}/v1` }
])
const steps = computed(() => [
  { number: '01', title: t('home.prism.steps.accountTitle'), description: t('home.prism.steps.accountDescription') },
  { number: '02', title: t('home.prism.steps.keyTitle'), description: t('home.prism.steps.keyDescription') },
  { number: '03', title: t('home.prism.steps.connectTitle'), description: t('home.prism.steps.connectDescription') }
])
// 均为教程中已有接入说明的客户端。
const clients = computed(() => ['Claude Code', 'Codex', 'Cherry Studio', 'LobeChat', t('home.prism.steps.immersiveTranslate')])
const copiedId = shallowRef('')
let copiedTimer: ReturnType<typeof setTimeout> | undefined

async function copy(id: string, url: string) {
  if (!(await copyToClipboard(url, t('home.prism.steps.copySuccess')))) return
  copiedId.value = id
  if (copiedTimer !== undefined) clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => {
    copiedId.value = ''
    copiedTimer = undefined
  }, 1800)
}

onUnmounted(() => {
  if (copiedTimer !== undefined) clearTimeout(copiedTimer)
})
</script>

<template>
  <section class="steps-section" aria-labelledby="steps-title">
    <div class="steps-inner">
      <div class="steps-copy">
        <header data-reveal>
          <p class="landing-eyebrow">THREE STEPS TO YOUR FIRST CALL</p>
          <h2 id="steps-title" class="landing-title">{{ t('home.prism.steps.title') }}</h2>
          <p class="landing-lead">{{ t('home.prism.steps.description') }}</p>
        </header>
        <ol class="step-list">
          <li v-for="(step, index) in steps" :key="step.number" class="step-item" data-reveal :style="{ '--reveal-index': index + 1 }">
            <span class="step-number" aria-hidden="true">{{ step.number }}</span>
            <div class="step-text">
              <h3>{{ step.title }}</h3>
              <p>{{ step.description }}</p>
            </div>
          </li>
        </ol>
        <div class="step-footer" data-reveal style="--reveal-index: 4">
          <p class="step-clients">
            <span class="clients-label">{{ t('home.prism.steps.worksWith') }}</span>
            <span v-for="client in clients" :key="client" class="client-name">{{ client }}</span>
          </p>
          <a
            v-if="menuVisible('/tutorial')"
            href="/tutorial/import"
            target="_blank"
            rel="noopener noreferrer"
            class="guide-link"
            :aria-label="t('home.prism.steps.guideNewTab')"
          >
            {{ t('home.prism.steps.guide') }}<Icon name="externalLink" size="sm" aria-hidden="true" />
          </a>
        </div>
      </div>

      <div class="endpoint-card" data-reveal style="--reveal-index: 2">
        <p class="card-label">{{ t('home.prism.steps.endpoints') }}</p>
        <ul class="endpoint-list">
          <li v-for="endpoint in endpoints" :key="endpoint.id" class="endpoint-row">
            <span class="endpoint-name">{{ endpoint.name }}</span>
            <code class="endpoint-url">{{ endpoint.url }}</code>
            <button
              type="button"
              class="copy-button"
              :class="{ copied: copiedId === endpoint.id }"
              :aria-label="t('home.prism.steps.copyLabel', { name: endpoint.name })"
              @click="copy(endpoint.id, endpoint.url)"
            >
              <Icon :name="copiedId === endpoint.id ? 'check' : 'copy'" size="sm" aria-hidden="true" />
              <span>{{ copiedId === endpoint.id ? t('home.prism.steps.copied') : t('home.prism.steps.copy') }}</span>
            </button>
          </li>
        </ul>
        <div class="code-block">
          <p class="card-label">{{ t('home.prism.steps.example') }}</p>
          <pre><code><span class="code-keyword">export</span> ANTHROPIC_BASE_URL=<span class="code-string">"{{ apiBase }}"</span>
<span class="code-keyword">export</span> ANTHROPIC_AUTH_TOKEN=<span class="code-string">"sk-your-api-key"</span>
<span class="code-command">claude</span></code></pre>
        </div>
        <p class="card-note">{{ t('home.prism.steps.note') }}</p>
      </div>
    </div>
  </section>
</template>

<style scoped>
.steps-section { position: relative; display: flex; align-items: center; min-height: var(--landing-screen-height, 100svh); padding: clamp(28px, 6svh, 76px) 5vw; }
.steps-inner { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, clamp(480px, 30vw, 600px)); align-items: center; gap: clamp(40px, 5vw, 80px); width: min(clamp(1180px, 72vw, 1440px), 100%); margin: 0 auto; }
.step-list { margin: clamp(28px, 4svh, 40px) 0 0; padding: 0; list-style: none; border-bottom: 1px solid rgb(202 226 255 / 20%); }
.step-item { display: grid; grid-template-columns: 52px minmax(0, 1fr); padding: 16px 0; border-top: 1px solid rgb(202 226 255 / 20%); }
.step-number { padding-top: 5px; color: #8ea9c7; font-size: calc(13px * var(--landing-type-scale, 1)); letter-spacing: .12em; font-variant-numeric: tabular-nums; }
.step-text h3 { margin: 0; color: #f3f7ff; font-size: calc(clamp(21px, 1.2vw, 26px) * var(--landing-type-scale, 1)); font-weight: 500; line-height: 1.45; }
.step-text p { margin: 7px 0 0; color: #c4d1e3; font-size: calc(clamp(16px, .95vw, 19px) * var(--landing-type-scale, 1)); line-height: 1.75; text-wrap: pretty; }
.step-footer { display: flex; flex-direction: column; align-items: flex-start; gap: 18px; margin-top: 20px; }
.step-clients { display: flex; flex-wrap: wrap; align-items: center; gap: 9px 8px; margin: 0; }
.clients-label { margin-right: 6px; color: #8ea9c7; font-size: calc(13.5px * var(--landing-type-scale, 1)); }
.client-name { padding: 5px 13px; border: 1px solid rgb(206 229 255 / 17%); border-radius: 999px; background: rgb(214 234 255 / 4%); color: #d5e2f2; font-size: calc(13.5px * var(--landing-type-scale, 1)); line-height: 1.5; white-space: nowrap; }
.guide-link { display: inline-flex; align-items: center; gap: 9px; min-height: 44px; color: #f2f6ff; font-size: calc(17px * var(--landing-type-scale, 1)); font-weight: 500; text-decoration: none; transition: color 180ms ease; }
.guide-link :deep(svg) { width: 15px; height: 15px; }
.guide-link:hover { color: #b6dcff; }

/* 不使用 backdrop-filter：卡片位于逐帧更新的 WebGL 画面之上，背景模糊会让滚动时每帧重算 */
.endpoint-card { position: relative; min-width: 0; padding: 20px; border: 1px solid rgb(205 228 255 / 15%); border-radius: 22px; background: linear-gradient(180deg, rgb(14 32 58 / 86%), rgb(6 17 33 / 92%)); box-shadow: 0 40px 90px rgb(0 6 18 / 38%), inset 0 1px 0 rgb(255 255 255 / 6%); }
.card-label { margin: 0 0 14px; color: #8ea9c7; font-size: calc(13px * var(--landing-type-scale, 1)); letter-spacing: .08em; }
.endpoint-list { display: grid; gap: 10px; margin: 0; padding: 0; list-style: none; }
.endpoint-row { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: 6px 16px; padding: 14px 14px 14px 18px; border: 1px solid rgb(206 229 255 / 10%); border-radius: 14px; background: rgb(214 234 255 / 4%); }
.endpoint-name { grid-column: 1; color: #9fb4cd; font-size: calc(13px * var(--landing-type-scale, 1)); }
.endpoint-url { grid-column: 1; min-width: 0; color: #f2f6ff; font-family: 'Cascadia Mono', 'JetBrains Mono', 'SFMono-Regular', Consolas, 'Liberation Mono', monospace; font-size: calc(clamp(15px, .85vw, 17px) * var(--landing-type-scale, 1)); line-height: 1.5; overflow-wrap: anywhere; }
.copy-button { grid-column: 2; grid-row: 1 / span 2; display: inline-flex; align-items: center; justify-content: center; gap: 6px; min-width: 76px; height: 36px; padding: 0 14px; border: 1px solid rgb(206 229 255 / 22%); border-radius: 999px; background: transparent; color: #e6f0fb; font-size: calc(13.5px * var(--landing-type-scale, 1)); transition: background 180ms ease, border-color 180ms ease, color 180ms ease; }
.copy-button :deep(svg) { width: 15px; height: 15px; }
.copy-button:hover { background: rgb(214 234 255 / 10%); border-color: rgb(206 229 255 / 36%); }
.copy-button.copied { color: #a8e6c8; border-color: rgb(168 230 200 / 45%); background: rgb(168 230 200 / 8%); }
.code-block { margin-top: 12px; padding: 14px 16px 12px; border: 1px solid rgb(206 229 255 / 8%); border-radius: 14px; background: rgb(2 9 20 / 58%); }
.code-block .card-label { margin-bottom: 10px; }
.code-block pre { margin: 0; overflow-x: auto; color: #dbe7f5; font-family: 'Cascadia Mono', 'JetBrains Mono', 'SFMono-Regular', Consolas, 'Liberation Mono', monospace; font-size: calc(clamp(14.5px, .82vw, 16.5px) * var(--landing-type-scale, 1)); line-height: 1.65; white-space: pre; scrollbar-width: thin; }
.code-keyword { color: #8fc2ff; }
.code-string { color: #f4d6a6; }
.code-command { color: #f2f6ff; }
.card-note { margin: 14px 2px 0; color: #8ea9c7; font-size: calc(13px * var(--landing-type-scale, 1)); line-height: 1.6; }
@media (max-width: 960px) {
  .steps-section { padding: 24px 22px; }
  .steps-inner { grid-template-columns: minmax(0, 1fr); gap: 16px; }
  .step-list { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px; margin-top: 18px; }
  .step-item { display: block; padding: 12px 0; }
  .step-number { display: block; padding: 0; margin-bottom: 6px; font-size: 11px; }
  .step-text h3 { font-size: 14px; line-height: 1.4; }
  .step-text p { margin-top: 6px; font-size: 12px; line-height: 1.5; }
  .step-footer { flex-direction: row; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 4px 12px; margin-top: 10px; }
  .step-clients { gap: 5px; }
  .clients-label, .client-name { font-size: 11px; }
  .client-name { padding: 3px 8px; }
  .guide-link { min-height: 36px; font-size: 12px; }
  .endpoint-card { padding: 14px; border-radius: 14px; }
  .card-label, .endpoint-name, .card-note { font-size: 11px; }
  .card-label { margin-bottom: 8px; }
  .endpoint-list { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
  .endpoint-row { gap: 4px 8px; padding: 8px; border-radius: 9px; }
  .endpoint-url { grid-column: 1 / -1; font-size: 12px; line-height: 1.4; }
  .copy-button { grid-row: 1; min-width: 36px; height: 36px; padding: 0 6px; }
  .copy-button > span { display: none; }
  .copy-button :deep(svg) { width: 15px; height: 15px; }
  .code-block { margin-top: 8px; padding: 8px 10px; border-radius: 9px; }
  .code-block .card-label { margin-bottom: 5px; }
  .code-block pre { font-size: 11px; line-height: 1.5; }
  .card-note { margin-top: 8px; margin-bottom: 0; line-height: 1.4; }
}
@media (max-width: 480px) {
  .steps-section { padding-inline: 18px; }
  .step-list { gap: 12px; }
  .endpoint-card { padding: 10px; }
  .endpoint-row { gap: 4px; padding: 6px; }
  .endpoint-name { font-size: 10px; }
}
@media (max-width: 960px) and (max-height: 740px) {
  .steps-section { padding-block: 12px; }
}
@media (prefers-reduced-motion: reduce) { .guide-link, .copy-button { transition: none; } }
</style>
