<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { accountsAPI } from '@/api/admin/accounts'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ modelValue: number[] }>()
const emit = defineEmits<{ 'update:modelValue': [ids: number[]] }>()
const { t } = useI18n()
const choice = shallowRef('')
const loading = shallowRef(false)
const error = shallowRef('')
const items = shallowRef<Array<{ value: string; label: string }>>([])
const names = shallowRef<Record<number, string>>({})
let controller: AbortController | null = null
let revision = 0

const options = computed(() => items.value.filter(item => !props.modelValue.includes(Number(item.value))))
const selected = computed(() => props.modelValue.map(id => ({ id, label: names.value[id] || `#${id}` })))

async function search(query = '') {
  controller?.abort()
  const requestController = new AbortController()
  controller = requestController
  const current = ++revision
  loading.value = true
  error.value = ''
  try {
    const result = await accountsAPI.list(1, 30, { search: query, lite: 'true' }, { signal: requestController.signal })
    if (current !== revision) return
    const labels = { ...names.value }
    items.value = result.items.map(account => {
      const label = `${account.name} · ${account.platform} · #${account.id}`
      labels[account.id] = label
      return { value: String(account.id), label }
    })
    names.value = labels
  } catch (err) {
    if (current === revision && !requestController.signal.aborted) {
      error.value = extractApiErrorMessage(err, t('admin.riskControl.accountAllowlistLoadError'))
    }
  } finally {
    if (current === revision) loading.value = false
  }
}

function add(value: unknown) {
  const id = Number(value)
  if (Number.isSafeInteger(id) && id > 0 && !props.modelValue.includes(id)) {
    emit('update:modelValue', [...props.modelValue, id])
  }
  choice.value = ''
}

watch(() => props.modelValue, async (ids, _previous, onCleanup) => {
  let active = true
  onCleanup(() => { active = false })
  const missing = ids.filter(id => !names.value[id])
  if (!missing.length) return
  // 名称回填限制为四个并发，不保存账号详情与凭据。
  for (let offset = 0; offset < missing.length && active; offset += 4) {
    const batch = missing.slice(offset, offset + 4)
    const results = await Promise.allSettled(batch.map(id => accountsAPI.getById(id)))
    if (!active) return
    const labels = { ...names.value }
    results.forEach((result, index) => {
      const id = batch[index]
      labels[id] = `#${id}`
      if (result.status !== 'fulfilled' || result.value.id !== id) return
      const account = result.value
      labels[id] = `${account.name} · ${account.platform} · #${account.id}`
    })
    names.value = labels
  }
}, { immediate: true })

onBeforeUnmount(() => { revision++; controller?.abort() })
onMounted(() => { void search() })
</script>

<template>
  <section class="space-y-3 border-t border-gray-100 pt-4 dark:border-dark-700" data-test="account-audit-allowlist">
    <div class="flex items-center justify-between gap-3">
      <label class="input-label mb-0">{{ t('admin.riskControl.accountAllowlist') }}</label>
      <span class="text-xs tabular-nums text-gray-500">{{ modelValue.length }}</span>
    </div>
    <Select
      v-model="choice"
      :options="options"
      :placeholder="t('admin.riskControl.accountAllowlistSearch')"
      :loading="loading"
      searchable
      remote
      @search="search"
      @change="add"
    />
    <p v-if="error" role="alert" class="text-xs text-red-600 dark:text-red-400">
      {{ error }} <button type="button" class="underline" @click="search()">{{ t('common.refresh') }}</button>
    </p>
    <div v-if="selected.length" class="flex flex-wrap gap-2">
      <button
        v-for="account in selected"
        :key="account.id"
        type="button"
        class="inline-flex max-w-full items-center gap-1.5 rounded-lg bg-gray-100 px-2.5 py-1.5 text-xs text-gray-700 hover:bg-gray-200 focus-visible:ring-2 focus-visible:ring-primary-500 dark:bg-dark-700 dark:text-gray-200 dark:hover:bg-dark-600"
        :aria-label="t('admin.riskControl.removeExemptAccount', { name: account.label })"
        @click="emit('update:modelValue', modelValue.filter(id => id !== account.id))"
      >
        <span class="truncate">{{ account.label }}</span><Icon name="x" size="xs" />
      </button>
    </div>
  </section>
</template>
