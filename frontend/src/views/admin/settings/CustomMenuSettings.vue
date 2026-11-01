<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'
import ImageUpload from '@/components/common/ImageUpload.vue'
import { nativeMenuIDs } from '@/router/workspace'
import type { MenuVisibility } from '@/config/menuPages'
import type { CustomMenuItem } from '@/types'

const props = defineProps<{ items: CustomMenuItem[]; visibility: MenuVisibility }>()
const emit = defineEmits<{
  'update:items': [items: CustomMenuItem[]]
  'update:visibility': [value: MenuVisibility]
}>()
const { t } = useI18n()
function update(index: number, patch: Partial<CustomMenuItem>) {
  emit('update:items', props.items.map((item, i) => i === index ? { ...item, ...patch } : item))
}
function add() {
  emit('update:items', [...props.items, { id: crypto.randomUUID().replace(/-/g, ''), label: '', url: '', icon_svg: '', visibility: 'user', sort_order: props.items.length }])
}
function remove(index: number) {
  emit('update:items', props.items.filter((_, i) => i !== index).map((item, sort_order) => ({ ...item, sort_order })))
}
function move(index: number, offset: number) {
  const target = index + offset
  if (target < 0 || target >= props.items.length) return
  const items = [...props.items]
  ;[items[index], items[target]] = [items[target], items[index]]
  emit('update:items', items.map((item, sort_order) => ({ ...item, sort_order })))
}
function setVisible(id: string, value: boolean) {
  emit('update:visibility', { ...props.visibility, [`/custom/${id}`]: value })
}
</script>

<template>
  <section class="card" aria-labelledby="custom-menus-title">
    <div class="flex items-center justify-between gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 id="custom-menus-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.customMenu.title') }}</h2>
      <button type="button" class="btn btn-secondary" :disabled="items.length >= 20" @click="add"><Icon name="plus" size="sm" />{{ t('admin.settings.customMenu.add') }}</button>
    </div>
    <div class="divide-y divide-gray-100 dark:divide-dark-700">
      <div v-for="(item, index) in items" :key="item.id || index" class="space-y-4 px-6 py-5">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex min-w-0 items-center gap-3">
            <span class="truncate text-sm font-medium text-gray-800 dark:text-gray-200">{{ item.label || t('admin.settings.customMenu.itemLabel', { n: index + 1 }) }}</span>
            <span v-if="nativeMenuIDs.has(item.id)" class="text-xs text-gray-500">{{ t('admin.settings.menus.nativeReplacement') }}</span>
            <Toggle v-else :model-value="visibility[`/custom/${item.id}`] !== false" :aria-label="`${item.label} ${t('admin.settings.menus.visible')}`" @update:model-value="setVisible(item.id, $event)" />
          </div>
          <div class="flex items-center gap-1">
            <button type="button" class="btn-ghost btn-icon" :disabled="index === 0" :aria-label="t('admin.settings.customMenu.moveUp')" @click="move(index, -1)"><Icon name="chevronUp" size="sm" /></button>
            <button type="button" class="btn-ghost btn-icon" :disabled="index === items.length - 1" :aria-label="t('admin.settings.customMenu.moveDown')" @click="move(index, 1)"><Icon name="chevronDown" size="sm" /></button>
            <button type="button" class="btn-ghost btn-icon text-red-500" :aria-label="t('admin.settings.customMenu.remove')" @click="remove(index)"><Icon name="trash" size="sm" /></button>
          </div>
        </div>
        <div class="grid gap-4 sm:grid-cols-2">
          <label class="space-y-1 text-xs font-medium text-gray-600 dark:text-gray-400">
            <span>{{ t('admin.settings.customMenu.name') }}</span>
            <input :value="item.label" type="text" maxlength="50" class="input text-sm" @input="update(index, { label: ($event.target as HTMLInputElement).value })">
          </label>
          <label class="space-y-1 text-xs font-medium text-gray-600 dark:text-gray-400">
            <span>{{ t('admin.settings.customMenu.visibility') }}</span>
            <select :value="item.visibility" class="input text-sm" @change="update(index, { visibility: ($event.target as HTMLSelectElement).value as 'user' | 'admin' })">
              <option value="user">{{ t('admin.settings.customMenu.visibilityUser') }}</option><option value="admin">{{ t('admin.settings.customMenu.visibilityAdmin') }}</option>
            </select>
          </label>
          <label class="space-y-1 text-xs font-medium text-gray-600 dark:text-gray-400 sm:col-span-2">
            <span>{{ t('admin.settings.customMenu.url') }}</span>
            <input :value="item.url" type="text" maxlength="2048" class="input text-sm" :placeholder="t('admin.settings.menus.urlPlaceholder')" @input="update(index, { url: ($event.target as HTMLInputElement).value })">
          </label>
          <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300 sm:col-span-2">
            <input :checked="item.hide_open_button" type="checkbox" data-testid="custom-menu-hide-open-button" @change="update(index, { hide_open_button: ($event.target as HTMLInputElement).checked })">
            {{ t('admin.settings.customMenu.hideOpenButton') }}
          </label>
          <div class="sm:col-span-2">
            <span class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">{{ t('admin.settings.customMenu.iconSvg') }}</span>
            <ImageUpload :model-value="item.icon_svg" mode="svg" size="sm" :upload-label="t('admin.settings.customMenu.uploadSvg')" :remove-label="t('admin.settings.customMenu.removeSvg')" @update:model-value="update(index, { icon_svg: $event })" />
          </div>
        </div>
      </div>
      <p v-if="!items.length" class="px-6 py-8 text-center text-sm text-gray-500">{{ t('admin.settings.menus.noCustomPages') }}</p>
    </div>
  </section>
</template>
