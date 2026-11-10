<script setup lang="ts">
import { computed } from 'vue'
import { validModelPlazaRechargeRatio, type ModelPlazaCurrency } from '@/utils/modelPlazaPricing'

const props = defineProps<{ modelValue: ModelPlazaCurrency; creditsPerCny?: number }>()
const emit = defineEmits<{ 'update:modelValue': [value: ModelPlazaCurrency] }>()
const canUseCny = computed(() => validModelPlazaRechargeRatio(props.creditsPerCny))
</script>

<template>
  <div class="plaza-currency-toggle" role="group" aria-label="实付价格币种">
    <button type="button" :aria-pressed="modelValue === 'usd'" @click="emit('update:modelValue', 'usd')">美元</button>
    <button type="button" :aria-pressed="modelValue === 'cny'" :disabled="!canUseCny" :title="canUseCny ? `1 元 = ${creditsPerCny} 额度` : '管理员尚未设置充值比例'" @click="emit('update:modelValue', 'cny')">人民币</button>
  </div>
</template>

<style scoped>
.plaza-currency-toggle { display: inline-flex; flex-shrink: 0; gap: 2px; padding: 3px; border-radius: 8px; background: var(--ws-soft, rgb(148 163 184 / .12)); }
.plaza-currency-toggle button { min-height: 32px; padding: 6px 10px; border-radius: 6px; font-size: 12px; color: var(--ws-muted, #64748b); white-space: nowrap; }
.plaza-currency-toggle button[aria-pressed='true'] { color: var(--ws-text, #0f766e); background: var(--ws-surface, rgb(255 255 255 / .8)); box-shadow: 0 1px 3px #00000012; }
.plaza-currency-toggle button:disabled { cursor: not-allowed; opacity: .45; }
.plaza-currency-toggle button:focus-visible { outline: 2px solid var(--ws-primary, #0d9488); outline-offset: 2px; }
</style>
