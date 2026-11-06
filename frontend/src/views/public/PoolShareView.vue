<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import UserPoolPanel from '@/components/pool/UserPoolPanel.vue'

const route = useRoute()
const { t } = useI18n()
// New links use query parameters for menu import compatibility; old fragment links remain valid.
const fragment = computed(() => new URLSearchParams(route.hash.slice(1)))
const token = computed(() => route.query.token !== undefined
  ? typeof route.query.token === 'string' ? route.query.token : ''
  : fragment.value.get('token') || '')
const initialTab = computed(() => (route.query.tab ?? fragment.value.get('tab')) === 'usage' ? 'usage' : 'accounts')
</script>

<template>
  <main class="min-h-screen bg-gray-50 px-4 py-8 dark:bg-dark-950">
    <div class="mx-auto max-w-screen-2xl">
      <header class="mb-6"><h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('admin.pool.title') }}</h1></header>
      <UserPoolPanel shared :token="token" :initial-tab="initialTab" />
    </div>
  </main>
</template>
