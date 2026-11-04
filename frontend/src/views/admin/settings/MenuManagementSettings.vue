<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import CustomMenuSettings from './CustomMenuSettings.vue'
import { isMenuVisible, menuPageGroups, type MenuVisibility, type MenuLabels } from '@/config/menuPages'
import type { CustomMenuItem } from '@/types'

const props = defineProps<{ visibility: MenuVisibility; customItems: CustomMenuItem[]; labels?: MenuLabels }>()
const emit = defineEmits<{
  'update:labels': [value: MenuLabels]
  'update:visibility': [value: MenuVisibility]
  'update:customItems': [value: CustomMenuItem[]]
}>()
const { t, locale } = useI18n()
const search = shallowRef('')
const chinese = computed(() => locale.value.startsWith('zh'))
const groups = computed(() => menuPageGroups.map(group => ({
  ...group, label: chinese.value ? group.zh : group.en,
  pages: group.pages.map(page => ({ ...page, label: chinese.value ? page.zh : page.en }))
    .filter(page => [page.zh, page.en, page.path, props.labels?.[page.path]].join(" ").toLowerCase().includes(search.value.trim().toLowerCase())),
})).filter(group => group.pages.length))
function setLabel(path: string, label: string) {
  const next = { ...(props.labels ?? {}) }
  if (label.trim()) next[path] = label
  else delete next[path]
  emit('update:labels', next)
}
function setVisible(path: string, visible: boolean) {
  emit('update:visibility', { ...props.visibility, [path]: visible })
}
</script>

<template>
  <section class="card" aria-labelledby="menu-management-title">
    <div class="flex flex-wrap items-center justify-between gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <div>
        <h2 id="menu-management-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.menus.title') }}</h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.menus.hint') }}</p>
      </div>
      <div class="flex flex-wrap items-center gap-2"><button type="button" class="btn btn-secondary" :disabled="!Object.keys(labels ?? {}).length" @click="emit('update:labels', {})">恢复全部名称</button><input v-model="search" type="search" class="input w-full sm:max-w-56" :placeholder="t('admin.settings.menus.search')" :aria-label="t('admin.settings.menus.search')"></div>
    </div>
    <div class="divide-y divide-gray-100 dark:divide-dark-700">
      <section v-for="group in groups" :key="group.id" class="px-6 py-5" :aria-labelledby="`menu-group-${group.id}`">
        <h3 :id="`menu-group-${group.id}`" class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">{{ group.label }}</h3>
        <div class="grid gap-x-8 xl:grid-cols-2">
          <div v-for="page in group.pages" :key="page.path" class="flex min-w-0 items-center justify-between gap-3 py-2.5">
            <div class="min-w-0 flex-1">
              <label :id="'menu-label-' + page.path" class="mb-1 block text-xs text-gray-500 dark:text-gray-400">{{ page.label }}</label>
              <div class="flex items-center gap-2"><input :value="labels?.[page.path] ?? ''" :placeholder="page.label" :aria-label="page.label + '显示名称'" maxlength="50" class="input min-w-0" @input="setLabel(page.path, ($event.target as HTMLInputElement).value)" /><button class="btn btn-ghost btn-sm shrink-0" type="button" :disabled="!labels?.[page.path]" @click="setLabel(page.path, '')">恢复默认</button></div>
            </div>
            <Toggle :model-value="isMenuVisible(page.path, visibility)" :aria-labelledby="`menu-label-${page.path}`" @update:model-value="setVisible(page.path, $event)" />
          </div>
        </div>
      </section>
      <p v-if="!groups.length" class="px-6 py-8 text-center text-sm text-gray-500">{{ t('admin.settings.menus.empty') }}</p>
    </div>
  </section>
  <CustomMenuSettings :items="customItems" :visibility="visibility" @update:items="emit('update:customItems', $event)" @update:visibility="emit('update:visibility', $event)" />
</template>
