<script setup lang="ts">
import { computed, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import { modelPrice, modelOfficialPrice, mediaPrice, type ModelEntry } from '@/composables/workspace/useModelSquare'
import type { ModelPlazaTokenPriceKey } from '@/utils/modelPlazaPricing'
import { platformLabel } from '@/utils/platformColors'
const props = defineProps<{ entry: ModelEntry }>()
const emit = defineEmits<{ detail: [] }>()
const copied = ref(false), copyError = ref(false)
const billing = computed(() => props.entry.offers[0]?.model.pricing?.billing_mode || 'token')
const priceFields: { label: string; key: ModelPlazaTokenPriceKey }[] = [
  { label: '输入', key: 'input_price' },
  { label: '输出', key: 'output_price' },
  { label: '缓存读取', key: 'cache_read_price' },
  { label: '缓存创建', key: 'cache_write_price' },
  { label: '缓存创建（1h）', key: 'cache_write_1h_price' }
]
const rows = computed(() => priceFields.map(field => ({
  ...field,
  paid: modelPrice(props.entry, field.key),
  official: modelOfficialPrice(props.entry, field.key)
})).filter(row => row.paid !== '—' || row.official !== '—'))
async function copy() {
  try {
    await navigator.clipboard.writeText(props.entry.name)
    copied.value = true
    copyError.value = false
  } catch {
    copyError.value = true
  }
}
</script>

<template>
  <article class="model-card" :data-platform="entry.platform">
    <header class="model-card-heading">
      <div class="model-icon"><ProviderIcon :provider="entry.platform" :size="29" /></div>
      <div class="model-identity"><span>{{ platformLabel(entry.platform) }}</span><button class="model-name" @click="emit('detail')">{{ entry.name }}</button></div>
      <button class="copy-model" :aria-label="'复制模型名 ' + entry.name" @click="copy"><Icon :name="copied ? 'check' : 'copy'" size="sm" /></button>
    </header>
    <div v-if="billing === 'token'" class="model-prices">
      <div v-if="rows.length" class="model-price-head"><span>/ 1M</span><span>实付价格</span><span>官方价格</span></div>
      <div v-for="row in rows" :key="row.key" class="model-price-row" :data-price-field="row.key"><span>{{ row.label }}</span><strong>{{ row.paid }}</strong><span class="model-original-price">{{ row.official }}</span></div>
      <p v-if="!rows.length" class="ws-muted">查看分组计价</p>
    </div>
    <div v-else class="model-media"><strong>{{ mediaPrice(entry) }}</strong><span>{{ billing === 'image' ? '/ 张' : '/ 次' }}</span></div>
    <footer class="model-footer">
      <span v-if="copied" class="copy-feedback" role="status">模型名已复制</span>
      <span v-else-if="copyError" class="copy-feedback" role="alert">请手动复制模型名</span>
      <span v-else class="model-offers">{{ entry.offers.length }} 个分组<span class="price-caption"> · 最低实付价</span></span>
      <button @click="emit('detail')">计价详情<Icon name="arrowRight" size="xs" /></button>
    </footer>
  </article>
</template>

<style scoped>
.model-card { --model-accent: #438aa1; display: flex; flex-direction: column; padding: 21px; border-radius: 17px; background: var(--ws-surface); box-shadow: 0 0 0 1px var(--ws-line), 0 5px 15px #00132b04; transition: box-shadow .2s, transform .2s; }
.model-card[data-platform='anthropic'] { --model-accent: #d18a6c; }
.model-card[data-platform='openai'], .model-card[data-platform='openai_bps'] { --model-accent: #4cab8c; }
.model-card[data-platform='gemini'] { --model-accent: #6b9cde; }
.model-card:hover { transform: translateY(-2px); box-shadow: 0 0 0 1px color-mix(in srgb, var(--model-accent) 45%, var(--ws-line)), 0 9px 25px #00132b0a; }
.model-card-heading { display: flex; align-items: center; gap: 13px; min-width: 0; }
.model-icon { display: grid; place-items: center; width: 48px; height: 48px; flex-shrink: 0; background: color-mix(in srgb, var(--model-accent) 10%, var(--ws-surface)); color: var(--model-accent); border-radius: 13px; }
.model-identity { min-width: 0; flex: 1; }
.model-identity > span { display: block; color: var(--ws-muted); font-size: 10px; margin-bottom: 4px; }
.model-name { text-align: left; font-size: 17px; font-weight: 650; line-height: 1.45; overflow-wrap: anywhere; }
.model-name:hover { color: var(--model-accent); }
.copy-model { display: grid; place-items: center; flex-shrink: 0; align-self: flex-start; width: 28px; height: 28px; color: var(--ws-muted); border-radius: 7px; }
.copy-model:hover { background: var(--ws-soft); color: var(--ws-text); }
.copy-model:focus-visible, .model-name:focus-visible, .model-footer button:focus-visible { outline: 2px solid var(--ws-primary); outline-offset: 3px; }
.model-prices { display: grid; gap: 9px; margin: 21px 0 20px; }
.model-prices > div { display: grid; grid-template-columns: minmax(0, 1fr) minmax(65px, auto) minmax(65px, auto); align-items: baseline; gap: 9px; font-size: 12px; }
.model-prices span { color: var(--ws-muted); }
.model-prices strong, .model-original-price { text-align: right; font-variant-numeric: tabular-nums; }
.model-prices strong { font-weight: 600; }
.model-prices > .model-price-head { font-size: 10px; }
.model-price-head > span:not(:first-child) { text-align: right; }
.model-media { padding: 24px 0; }
.model-media strong { font-size: 26px; font-weight: 550; }
.model-media > span { color: var(--ws-muted); font-size: 11px; margin-left: 7px; }
.model-footer { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-top: auto; padding-top: 14px; border-top: 1px solid var(--ws-line); }
.model-offers { color: var(--model-accent); font-size: 11px; white-space: nowrap; }
.price-caption, .copy-feedback { color: var(--ws-muted); font-size: 10px; }
.model-footer button { display: inline-flex; align-items: center; gap: 4px; color: var(--ws-muted); font-size: 11px; white-space: nowrap; }
.model-footer button:hover { color: var(--ws-primary); }
@media (max-width: 600px) { .model-card { padding: 19px; } }
</style>
