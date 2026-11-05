<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getPoolDisplaySettings as getSettings, savePoolDisplaySettings as updateSettings } from '@/api/pool'
import { getAll as listGroups } from '@/api/admin/groups'
import { usePoolRequest } from '@/composables/usePoolRequest'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import PoolNetworkError from './PoolNetworkError.vue'
import { useAppStore } from '@/stores'
const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n(), app = useAppStore()
const read = usePoolRequest<{ settings: Awaited<ReturnType<typeof getSettings>>; groups: Awaited<ReturnType<typeof listGroups>> } | null>(null)
const { loading, failed } = read
const saving = ref(false), saveFailed = ref(false)
const config = ref({ count: 0, banned: 0, platform: 'anthropic', groups: [] as number[] })
const groups = computed(() => read.data.value?.groups ?? [])
const platforms = [{ value:'anthropic', label:'Anthropic' }, { value:'openai', label:'OpenAI' }, { value:'gemini', label:'Gemini' }, { value:'claude', label:'Claude' }]
async function load() {
  saveFailed.value = false
  if (await read.run(async () => { const [settings, groups] = await Promise.all([getSettings(), listGroups()]); return { settings, groups } })) {
    const settings = read.data.value!.settings
    config.value = { count:settings.pool_user_synthetic_count ?? 0, banned:settings.pool_user_synthetic_banned ?? 0, platform:settings.pool_user_synthetic_platform || 'anthropic', groups:settings.pool_user_visible_group_ids ?? [] }
  }
}
async function save() {
  if (saving.value || loading.value || failed.value || !read.data.value) return
  saving.value = true; saveFailed.value = false
  const count = Math.max(0, Math.min(50, Math.trunc(Number(config.value.count) || 0)))
  const banned = Math.max(0, Math.min(count, Math.trunc(Number(config.value.banned) || 0)))
  try {
    await updateSettings({ pool_user_synthetic_count:count, pool_user_synthetic_banned:banned, pool_user_synthetic_platform:config.value.platform, pool_user_visible_group_ids:config.value.groups })
    app.showSuccess(t('admin.pool.settingsSaved')); emit('saved'); emit('close')
  } catch { saveFailed.value = true }
  finally { saving.value = false }
}
function close() { if (!saving.value) emit('close') }
watch(() => props.show, show => { read.reset(); saveFailed.value = false; if (show) void load() }, { immediate:true })
</script>

<template>
  <BaseDialog :show="show" :title="t('admin.pool.extra.synthetic.title')" width="normal" :show-close-button="!saving" :close-on-escape="!saving" @close="close">
    <form id="pool-display-settings" class="space-y-5" @submit.prevent="save">
      <PoolNetworkError v-if="failed" :busy="loading" @retry="load" />
      <div v-if="saveFailed" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ t('admin.pool.networkError') }} · {{ t('admin.pool.extra.synthetic.saveFailed') }}</div>
      <fieldset class="space-y-4" :disabled="loading || saving || failed || !read.data.value">
        <div class="grid grid-cols-2 gap-4">
          <label class="text-sm text-gray-700 dark:text-dark-200">{{ t('admin.pool.extra.synthetic.count') }}<input v-model.number="config.count" type="number" min="0" max="50" step="1" required class="input mt-1.5" /><span class="mt-1 block text-xs text-gray-500 dark:text-dark-400">{{ t('admin.pool.extra.synthetic.countHint') }}</span></label>
          <label class="text-sm text-gray-700 dark:text-dark-200">{{ t('admin.pool.extra.synthetic.banned') }}<input v-model.number="config.banned" type="number" min="0" :max="config.count" step="1" required class="input mt-1.5" /></label>
        </div>
        <div><label id="pool-platform-label" class="input-label">{{ t('admin.pool.extra.synthetic.platform') }}</label><Select v-model="config.platform" :options="platforms" :disabled="loading || saving || failed" aria-labelledby="pool-platform-label" /></div>
        <div><p id="pool-group-label" class="input-label">{{ t('admin.pool.extra.synthetic.visibleGroup') }}</p><div class="max-h-52 space-y-2 overflow-auto rounded-xl border border-gray-200 p-3 dark:border-dark-700" role="group" aria-labelledby="pool-group-label"><label v-for="group in groups" :key="group.id" class="flex cursor-pointer items-center gap-2 text-sm"><input v-model="config.groups" type="checkbox" :value="group.id" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />{{ group.name }}</label></div><p class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.pool.groupScopeHint') }}</p></div>
      </fieldset>
    </form>
    <template #footer>
      <button type="button" class="btn btn-secondary" :disabled="saving" @click="close">{{ t('common.cancel') }}</button>
      <button form="pool-display-settings" type="submit" class="btn btn-primary" :disabled="loading || saving || failed || !read.data.value">{{ loading ? t('common.loading') : saving ? t('admin.pool.saving') : t('admin.pool.extra.synthetic.save') }}</button>
    </template>
  </BaseDialog>
</template>
