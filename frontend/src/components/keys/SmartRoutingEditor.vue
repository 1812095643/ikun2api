<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Group } from '@/types'
import Select from '@/components/common/Select.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'

const props = defineProps<{ modelValue: number[]; groups: Group[]; userRates: Record<number, number> }>()
const emit = defineEmits<{ 'update:modelValue': [ids: number[]] }>()
const { t } = useI18n()
const selected = shallowRef<number | null>(null)
const rows = computed(() => props.modelValue.map(id => ({ id, group: props.groups.find(group => group.id === id) })))
const options = computed(() => props.groups.filter(group => !props.modelValue.includes(group.id)).map(group => ({ value: group.id, label: group.name })))
function add(value: string | number | boolean | null) {
  const id = Number(value)
  if (props.groups.some(group => group.id === id) && !props.modelValue.includes(id)) emit('update:modelValue', [...props.modelValue, id])
  selected.value = null
}
function move(index: number, delta: number) {
  const ids = [...props.modelValue]
  const target = index + delta
  if (target < 0 || target >= ids.length) return
  ;[ids[index], ids[target]] = [ids[target], ids[index]]
  emit('update:modelValue', ids)
}
function remove(index: number) { emit('update:modelValue', props.modelValue.filter((_, i) => i !== index)) }
</script>

<template>
  <div class="space-y-2" data-testid="smart-routing-editor">
    <ol v-if="rows.length" class="max-h-72 space-y-2 overflow-y-auto" :aria-label="t('keys.smartRouting')">
      <li v-for="(row, index) in rows" :key="row.id" class="flex min-w-0 items-center gap-2 rounded-xl border border-gray-200 px-3 py-2.5 dark:border-dark-600" data-testid="smart-route-row">
        <span class="w-5 shrink-0 text-center text-xs font-medium tabular-nums text-gray-400">{{ index + 1 }}</span>
        <div class="min-w-0 flex-1">
          <GroupBadge v-if="row.group" :name="row.group.name" :platform="row.group.platform" :subscription-type="row.group.subscription_type" :rate-multiplier="row.group.rate_multiplier" :user-rate-multiplier="userRates[row.id]" />
          <span v-else class="text-sm text-gray-500">{{ t('keys.smartRoutingUnavailable', { id: row.id }) }}</span>
        </div>
        <div class="flex shrink-0 items-center gap-0.5">
          <button type="button" class="btn btn-ghost btn-sm !px-2 disabled:opacity-30" :disabled="index === 0" :aria-label="t('keys.smartRoutingMoveUp')" @click="move(index, -1)">↑</button>
          <button type="button" class="btn btn-ghost btn-sm !px-2 disabled:opacity-30" :disabled="index === rows.length - 1" :aria-label="t('keys.smartRoutingMoveDown')" @click="move(index, 1)">↓</button>
          <button type="button" class="btn btn-ghost btn-sm !px-2 text-gray-400 hover:text-red-500" :aria-label="t('keys.smartRoutingRemove')" @click="remove(index)">×</button>
        </div>
      </li>
    </ol>
    <Select :model-value="selected" :options="options" :placeholder="t('keys.smartRoutingAdd')" :searchable="true" :search-placeholder="t('keys.searchGroup')" :empty-text="t('common.noGroupsAvailable')" data-testid="smart-route-add" @update:model-value="add" />
    <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('keys.smartRoutingHint') }}</p>
  </div>
</template>
