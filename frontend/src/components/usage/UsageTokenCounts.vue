<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { UsageLog } from '@/types'
import Icon from '@/components/icons/Icon.vue'
import { formatCacheTokens } from '@/utils/formatters'
import { hasImageInputTokens, hasImageOutputTokens } from '@/utils/imageUsage'

type TokenCounts = Pick<UsageLog,
  'input_tokens' | 'output_tokens' | 'cache_read_tokens' | 'cache_creation_tokens' |
  'cache_creation_5m_tokens' | 'cache_creation_1h_tokens' | 'image_input_tokens' | 'image_output_tokens'
> & { cache_ttl_overridden?: boolean }
defineProps<{ row: TokenCounts }>()
const { t } = useI18n()
</script>

<template>
  <div class="space-y-1 whitespace-nowrap text-sm tabular-nums">
    <div class="flex items-center gap-2">
      <div class="inline-flex items-center gap-1" :title="`${t('usage.in')}: ${row.input_tokens?.toLocaleString() || 0}`">
        <Icon name="arrowDown" size="sm" class="h-3.5 w-3.5 text-emerald-500" />
        <span class="font-medium text-gray-900 dark:text-white">{{ row.input_tokens?.toLocaleString() || 0 }}</span>
      </div>
      <div class="inline-flex items-center gap-1" :title="`${t('usage.out')}: ${row.output_tokens?.toLocaleString() || 0}`">
        <Icon name="arrowUp" size="sm" class="h-3.5 w-3.5 text-violet-500" />
        <span class="font-medium text-gray-900 dark:text-white">{{ row.output_tokens?.toLocaleString() || 0 }}</span>
      </div>
    </div>
    <div v-if="row.cache_read_tokens > 0 || row.cache_creation_tokens > 0" class="flex items-center gap-2">
      <div v-if="row.cache_read_tokens > 0" class="inline-flex items-center gap-1" :title="`${t('usage.cacheRead')}: ${row.cache_read_tokens.toLocaleString()}`">
        <svg class="h-3.5 w-3.5 text-sky-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" /></svg>
        <span class="font-medium text-sky-600 dark:text-sky-400">{{ formatCacheTokens(row.cache_read_tokens) }}</span>
      </div>
      <div v-if="row.cache_creation_tokens > 0" class="inline-flex items-center gap-1" :title="`${t('usage.cacheCreationTokensLabel')}: ${row.cache_creation_tokens.toLocaleString()}`">
        <svg class="h-3.5 w-3.5 text-amber-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
        <span class="font-medium text-amber-600 dark:text-amber-400">{{ formatCacheTokens(row.cache_creation_tokens) }}</span>
        <span v-if="row.cache_creation_1h_tokens > 0" class="inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-orange-100 text-orange-600 ring-1 ring-inset ring-orange-200 dark:bg-orange-500/20 dark:text-orange-400 dark:ring-orange-500/30">1h</span>
        <span v-if="row.cache_ttl_overridden" :title="t('usage.cacheTtlOverriddenHint')" class="inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-rose-100 text-rose-600 ring-1 ring-inset ring-rose-200 dark:bg-rose-500/20 dark:text-rose-400 dark:ring-rose-500/30 cursor-help">R</span>
      </div>
    </div>
    <div v-if="hasImageInputTokens(row)" class="flex items-center gap-2">
      <div class="inline-flex items-center gap-1">
        <svg class="h-3.5 w-3.5 text-fuchsia-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" /></svg>
        <span class="font-medium text-fuchsia-600 dark:text-fuchsia-400">{{ row.image_input_tokens.toLocaleString() }}</span>
      </div>
    </div>
    <div v-if="hasImageOutputTokens(row)" class="flex items-center gap-2">
      <div class="inline-flex items-center gap-1">
        <svg class="h-3.5 w-3.5 text-pink-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" /></svg>
        <span class="font-medium text-pink-600 dark:text-pink-400">{{ row.image_output_tokens.toLocaleString() }}</span>
      </div>
    </div>
  </div>
</template>
