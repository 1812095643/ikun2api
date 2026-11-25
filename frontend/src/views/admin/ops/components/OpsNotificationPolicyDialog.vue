<script setup lang="ts">
import { computed, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import { getAllIncludingInactive } from '@/api/admin/groups'
import type { MonitorRule, NotificationPolicy, NotificationTarget, NotificationChannel } from '@/api/admin/opsMonitor'

const props = defineProps<{ show: boolean; value: NotificationPolicy | null; targets: NotificationTarget[]; channels: NotificationChannel[]; rules: MonitorRule[] }>()
const emit = defineEmits<{ close: []; save: [policy: NotificationPolicy] }>()
const { t } = useI18n()
const form = ref<NotificationPolicy | null>(null), models = shallowRef(''), accounts = shallowRef(''), error = shallowRef('')
const groups = ref<{ id: number; name: string }[]>([])
const platforms = ['anthropic', 'openai', 'gemini', 'antigravity', 'grok']
const destinations = computed(() => props.targets.map(target => ({ ...target, label: `${props.channels.find(c => c.id === target.channel_id)?.name || ''} · ${target.name}` })))
function toggle<T extends string | number>(list: T[], value: T, checked: boolean) { return checked ? [...new Set([...list, value])] : list.filter(item => item !== value) }
watch(() => props.show, show => {
  if (!show) return
  form.value = props.value ? JSON.parse(JSON.stringify(props.value)) : { id: crypto.randomUUID(), name: '', enabled: true, rules: [], platforms: [], group_ids: [], account_ids: [], models: [], severities: ['P0', 'P1'], kinds: ['firing', 'escalated', 'resolved'], target_ids: [], fallback_target_ids: [], repeat_minutes: 0, mention_all: false }
  if (!form.value) return
  for (const key of ['rules', 'platforms', 'group_ids', 'account_ids', 'models', 'severities', 'kinds', 'target_ids', 'fallback_target_ids'] as const) form.value[key] ??= []
  models.value = form.value.models.join(', '); accounts.value = form.value.account_ids.join(', '); error.value = ''
  void getAllIncludingInactive().then(data => { groups.value = data }).catch(() => undefined)
}, { immediate: true })
function save() {
  if (!form.value) return
  const ids = accounts.value.split(/[,，\s]+/).filter(Boolean).map(Number)
  if (ids.some(id => !Number.isSafeInteger(id) || id <= 0)) { error.value = t('admin.ops.enhanced.accountValidation'); return }
  form.value.models = models.value.split(/[,，\s]+/).filter(Boolean); form.value.account_ids = ids
  if (!form.value.name.trim() || !form.value.target_ids.length) { error.value = t('admin.ops.enhanced.policyValidation'); return }
  emit('save', form.value); emit('close')
}
</script>

<template>
  <BaseDialog :show="show" :title="t('admin.ops.enhanced.policyConfig')" width="wide" :z-index="70" @close="emit('close')">
    <div v-if="form" class="space-y-4">
      <p v-if="error" class="text-sm text-red-600" role="alert">{{ error }}</p>
      <div class="flex items-center gap-3"><input v-model="form.name" class="input flex-1" :placeholder="t('admin.ops.enhanced.ruleName')" maxlength="100" /><Toggle v-model="form.enabled" /></div>
      <div><label class="input-label">{{ t('admin.ops.enhanced.severity') }}</label><div class="flex flex-wrap gap-4"><label v-for="value in ['P0', 'P1', 'P2', 'P3']" :key="value" class="flex items-center gap-2 text-sm"><input type="checkbox" class="checkbox" :checked="form.severities.includes(value)" @change="form.severities = toggle(form.severities, value, ($event.target as HTMLInputElement).checked)" />{{ value }}</label></div></div>
      <div><label class="input-label">{{ t('admin.ops.enhanced.changes') }}</label><div class="flex flex-wrap gap-4"><label v-for="value in ['firing', 'escalated', 'resolved']" :key="value" class="flex items-center gap-2 text-sm"><input type="checkbox" class="checkbox" :checked="form.kinds.includes(value)" @change="form.kinds = toggle(form.kinds, value, ($event.target as HTMLInputElement).checked)" />{{ t(`admin.ops.enhanced.notificationKinds.${value}`) }}</label></div></div>
      <div><label class="input-label">{{ t('admin.ops.enhanced.targets') }}</label><div class="grid gap-2 sm:grid-cols-2"><label v-for="target in destinations" :key="target.id" class="flex items-center gap-2 rounded-lg bg-gray-50 px-3 py-2 text-sm dark:bg-dark-800"><input type="checkbox" class="checkbox" :checked="form.target_ids.includes(target.id)" @change="form.target_ids = toggle(form.target_ids, target.id, ($event.target as HTMLInputElement).checked)" /><span class="truncate">{{ target.label }}</span><span v-if="!target.enabled" class="text-xs text-gray-400">{{ t('admin.ops.enhanced.disabled') }}</span></label></div><p v-if="!destinations.length" class="text-xs text-gray-400">{{ t('admin.ops.enhanced.createChannelFirst') }}</p></div>
      <details class="rounded-xl bg-gray-50 p-3 dark:bg-dark-800"><summary class="cursor-pointer text-sm">{{ t('admin.ops.enhanced.filters') }}</summary><div class="mt-3 space-y-4">
        <div><label class="input-label">{{ t('admin.ops.enhanced.platformAll') }}</label><div class="flex flex-wrap gap-3"><label v-for="value in platforms" :key="value" class="flex items-center gap-1.5 text-xs"><input type="checkbox" :checked="form.platforms.includes(value)" @change="form.platforms = toggle(form.platforms, value, ($event.target as HTMLInputElement).checked)" />{{ value === 'anthropic' ? 'Claude' : value }}</label></div></div>
        <div><label class="input-label">{{ t('admin.ops.enhanced.groupAll') }}</label><div class="grid max-h-36 gap-2 overflow-y-auto sm:grid-cols-2"><label v-for="group in groups" :key="group.id" class="flex items-center gap-2 text-xs"><input type="checkbox" :checked="form.group_ids.includes(group.id)" @change="form.group_ids = toggle(form.group_ids, group.id, ($event.target as HTMLInputElement).checked)" />{{ group.name }}</label></div></div>
        <div><label class="input-label">{{ t('admin.ops.enhanced.rulesAll') }}</label><div class="grid max-h-44 gap-2 overflow-y-auto sm:grid-cols-2"><label v-for="rule in rules" :key="rule.id" class="flex items-center gap-2 text-xs"><input type="checkbox" :checked="form.rules.includes(rule.id)" @change="form.rules = toggle(form.rules, rule.id, ($event.target as HTMLInputElement).checked)" /><span>{{ rule.id }} · {{ t(`admin.ops.enhanced.rules.${rule.id}`) }}</span></label></div></div>
        <div class="grid gap-3 sm:grid-cols-2"><div><label class="input-label">{{ t('admin.ops.enhanced.modelOptional') }}</label><input v-model="models" class="input" /></div><div><label class="input-label">{{ t('admin.ops.enhanced.accountOptional') }}</label><input v-model="accounts" class="input" inputmode="numeric" /></div></div>
      </div></details>
      <div><label class="input-label">{{ t('admin.ops.enhanced.fallback') }}</label><div class="flex flex-wrap gap-3"><label v-for="target in destinations.filter(item => !form?.target_ids.includes(item.id))" :key="target.id" class="flex items-center gap-1.5 text-xs"><input type="checkbox" :checked="form.fallback_target_ids.includes(target.id)" @change="form.fallback_target_ids = toggle(form.fallback_target_ids, target.id, ($event.target as HTMLInputElement).checked)" />{{ target.label }}</label></div></div>
      <div class="grid gap-3 sm:grid-cols-2"><div><label class="input-label">{{ t('admin.ops.enhanced.repeat') }}</label><input v-model.number="form.repeat_minutes" type="number" min="0" max="1440" class="input" /></div><label class="flex items-center justify-between gap-3 text-sm">{{ t('admin.ops.enhanced.mentionAll') }}<Toggle v-model="form.mention_all" /></label></div>
    </div>
    <template #footer><button class="btn btn-secondary" @click="emit('close')">{{ t('common.cancel') }}</button><button class="btn btn-primary" @click="save">{{ t('common.confirm') }}</button></template>
  </BaseDialog>
</template>
