<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import WorkspaceShell from '@/components/workspace/WorkspaceShell.vue'
import WorkspaceDialog from '@/components/workspace/WorkspaceDialog.vue'
import ModelFilters from '@/components/workspace/models/ModelFilters.vue'
import ModelCard from '@/components/workspace/models/ModelCard.vue'
import PlazaGroupSection from '@/components/modelPlaza/PlazaGroupSection.vue'
import { useModelSquare } from '@/composables/workspace/useModelSquare'
import { platformLabel } from '@/utils/platformColors'
import coverImage from '@/assets/workspace/model-cover.webp'

const { loading, error, search, platform, group, mode, selected, platforms,
  platformCounts, totalModels, groups, entries, filteredGroups, load } = useModelSquare()
function selectPlatform(value: string) {
  platform.value = value
  group.value = ''
}
function resetFilters() {
  platform.value = ''
  group.value = ''
  search.value = ''
}
</script>

<template>
  <WorkspaceShell class="workspace-discovery">
    <div class="catalog-layout">
      <ModelFilters :platforms="platforms" :counts="platformCounts" :total="totalModels"
        :groups="groups" :platform="platform" :group="group"
        @platform="selectPlatform" @group="group = $event" @reset="resetFilters" />
      <section class="catalog-content">
        <div class="catalog-cover" :style="{ backgroundImage: 'linear-gradient(100deg, #153f68dd, #204f8ca6), url(' + coverImage + ')' }">
          <div><h2>{{ platform ? platformLabel(platform) : '全部模型' }}</h2><span class="cover-count">{{ entries.length }} 个模型 · {{ groups.length }} 个分组</span></div>
          <div class="cover-mark" aria-hidden="true">
            <ProviderIcon v-if="platform" :provider="platform" :size="35" />
            <Icon v-else name="sparkles" size="xl" />
          </div>
        </div>
        <div class="catalog-toolbar">
          <label class="model-search"><Icon name="search" size="sm" /><input v-model="search" class="ws-input" placeholder="搜索模型名称" aria-label="搜索模型名称" /></label>
          <div class="model-view-toggle" role="group" aria-label="显示方式">
            <button :aria-pressed="mode === 'cards'" aria-label="模型卡片" @click="mode = 'cards'"><Icon name="grid" size="sm" /><span>卡片</span></button>
            <button :aria-pressed="mode === 'groups'" aria-label="分组价目表" @click="mode = 'groups'"><Icon name="list" size="sm" /><span>价目表</span></button>
          </div>
          <button class="ws-icon-btn" :disabled="loading" aria-label="刷新模型" @click="load">
            <Icon name="refresh" size="sm" :class="{ 'ws-spin': loading }" />
          </button>
        </div>
        <div v-if="error" class="ws-error" role="alert">{{ error }}<button @click="load">重试</button></div>
        <div v-if="loading && !entries.length" class="ws-empty" role="status"><Icon name="refresh" class="ws-spin" /><p>正在读取模型…</p></div>
        <div v-else-if="!entries.length" class="ws-empty"><Icon name="search" size="lg" /><h2>没有匹配的模型</h2><button class="ws-button" @click="resetFilters">重置筛选</button></div>
        <div v-else-if="mode === 'cards'" class="model-grid">
          <ModelCard v-for="entry in entries" :key="entry.platform + ':' + entry.name" :entry="entry" @detail="selected = entry" />
        </div>
        <div v-else class="space-y-5"><PlazaGroupSection v-for="item in filteredGroups" :key="item.id" :group="item" /></div>
      </section>
    </div>
    <WorkspaceDialog :open="!!selected" :title="selected?.name || '模型计价'" wide @update:open="selected = null">
      <div v-if="selected" class="space-y-5">
        <PlazaGroupSection v-for="offer in selected.offers" :key="offer.group.id" :group="{ ...offer.group, models: [offer.model] }" />
      </div>
    </WorkspaceDialog>
  </WorkspaceShell>
</template>

<style scoped>
.catalog-layout { display: grid; grid-template-columns: 180px minmax(0, 1fr); gap: 26px; align-items: start; }
.catalog-content { min-width: 0; }
.catalog-cover { display: flex; justify-content: space-between; align-items: center; gap: 20px; min-height: 116px; padding: 24px 28px; border-radius: 17px 17px 0 0; background-size: cover; background-position: center 48%; color: #f8fbff; }
.catalog-cover h2 { font-size: 23px; font-weight: 650; letter-spacing: -.5px; margin-bottom: 9px; }
.cover-count { font-size: 11px; padding: 4px 8px; background: #f1f6fc; color: #254665; border-radius: 6px; }
.cover-mark { display: grid; place-items: center; flex-shrink: 0; width: 63px; height: 63px; border-radius: 17px; color: #213b55; background: #f2f7f4eb; box-shadow: inset 0 1px #ffffffcc; }
.catalog-toolbar { display: flex; align-items: center; gap: 10px; padding: 12px; margin-bottom: 22px; background: var(--ws-surface); border-radius: 0 0 13px 13px; box-shadow: 0 1px 0 var(--ws-line); }
.model-search { position: relative; flex: 1; min-width: 90px; }
.model-search > svg { position: absolute; top: 12px; left: 12px; color: var(--ws-muted); }
.model-search input { padding-left: 37px; background: var(--ws-soft); border-color: transparent; }
.model-view-toggle { display: flex; flex-shrink: 0; padding: 3px; background: var(--ws-soft); border-radius: 8px; }
.model-view-toggle button { display: flex; align-items: center; gap: 6px; padding: 7px 9px; color: var(--ws-muted); border-radius: 6px; font-size: 11px; }
.model-view-toggle button[aria-pressed='true'] { color: var(--ws-text); background: var(--ws-surface); box-shadow: 0 1px 3px #00000012; }
.model-view-toggle button:focus-visible { outline: 2px solid var(--ws-primary); outline-offset: 2px; }
.catalog-toolbar > .ws-icon-btn { border-color: transparent; background: transparent; }
.model-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 17px; }
@media (min-width: 1700px) { .model-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); } }
@media (max-width: 1100px) { .catalog-layout { display: block; } }
@media (max-width: 650px) { .model-grid { grid-template-columns: 1fr; } .catalog-cover { min-height: 105px; padding: 20px; } .catalog-cover h2 { font-size: 21px; } .cover-mark { width: 51px; height: 51px; border-radius: 13px; } .catalog-toolbar { gap: 5px; padding: 10px; } .model-view-toggle button { min-height: 38px; padding: 7px; } .model-view-toggle button span { display: none; } }
</style>
