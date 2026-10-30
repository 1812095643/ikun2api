<script setup lang="ts">
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import { platformLabel } from '@/utils/platformColors'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
defineProps<{ platforms: string[]; counts: Record<string, number>; total: number; groups: ModelPlazaGroup[]; platform: string; group: string }>()
const emit = defineEmits<{ platform: [value: string]; group: [value: string]; reset: [] }>()
</script>

<template>
  <aside class="catalog-filters" aria-label="模型筛选">
    <div class="filter-heading"><span>筛选</span><button @click="emit('reset')">重置</button></div>
    <section class="filter-section">
      <h2>供应商</h2>
      <div class="filter-options">
        <button :aria-pressed="!platform" @click="emit('platform', '')"><span>全部供应商</span><small>{{ total }}</small></button>
        <button v-for="item in platforms" :key="item" :aria-pressed="platform === item" @click="emit('platform', item)">
          <ProviderIcon :provider="item" :size="17" /><span>{{ platformLabel(item) }}</span><small>{{ counts[item] }}</small>
        </button>
      </div>
    </section>
    <section class="filter-section">
      <h2>可用分组</h2>
      <div class="filter-options group-options">
        <button :aria-pressed="!group" @click="emit('group', '')"><span>全部分组</span><small>{{ groups.length }}</small></button>
        <button v-for="item in groups" :key="item.id" :aria-pressed="group === String(item.id)" :title="item.name" @click="emit('group', String(item.id))">
          <span>{{ item.name }}</span><small>{{ item.user_rate_multiplier ?? item.rate_multiplier }}×</small>
        </button>
      </div>
    </section>
  </aside>
</template>

<style scoped>
.catalog-filters { min-width: 0; }
.filter-heading { display: flex; justify-content: space-between; align-items: center; margin-bottom: 25px; font-size: 16px; font-weight: 650; }
.filter-heading > button { color: var(--ws-muted); font-size: 11px; font-weight: 400; padding: 4px 7px; border-radius: 6px; }
.filter-heading > button:hover { background: var(--ws-soft); }
.filter-section + .filter-section { margin-top: 28px; }
.filter-section h2 { color: var(--ws-muted); font-size: 11px; font-weight: 500; margin-bottom: 10px; padding-left: 8px; }
.filter-options { display: flex; flex-direction: column; gap: 7px; }
.filter-options button { display: flex; align-items: center; gap: 8px; min-height: 37px; padding: 8px 10px; border-radius: 9px; text-align: left; color: var(--ws-text); font-size: 12px; box-shadow: inset 0 0 0 1px var(--ws-line); transition: background .15s; }
.filter-options button > span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.filter-options small { flex-shrink: 0; margin-left: auto; color: var(--ws-muted); background: var(--ws-soft); border-radius: 5px; padding: 1px 5px; font-size: 10px; }
.filter-options button[aria-pressed='true'] { background: color-mix(in srgb, var(--ws-primary) 15%, transparent); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--ws-primary) 25%, transparent); color: var(--ws-primary); font-weight: 600; }
.filter-options button:hover { background: var(--ws-soft); }
.filter-options button:focus-visible, .filter-heading button:focus-visible { outline: 2px solid var(--ws-primary); outline-offset: 3px; }
@media (max-width: 1100px) {
  .filter-heading { display: none; }
  .catalog-filters { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin-bottom: 20px; }
  .filter-section + .filter-section { margin: 0; }
  .filter-options { flex-direction: row; flex-wrap: wrap; }
  .filter-options button { gap: 6px; }
  .filter-options button > span { max-width: 170px; }
}
@media (max-width: 650px) {
  .catalog-filters { grid-template-columns: 1fr; gap: 12px; }
  .filter-options { flex-wrap: nowrap; overflow-x: auto; padding: 2px; }
  .filter-options button { flex-shrink: 0; min-height: 40px; }
  .filter-section h2 { margin-bottom: 6px; }
}
</style>
