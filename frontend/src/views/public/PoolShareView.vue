<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import UserPoolPanel from '@/components/pool/UserPoolPanel.vue'
import { resolvePoolShareToken } from '@/utils/poolShareLink'

const route = useRoute()
const { t } = useI18n()
// share_token survives the host site's automatic injection of its login token.
const fragment = computed(() => new URLSearchParams(route.hash.slice(1)))
const token = computed(() => resolvePoolShareToken(route.query, route.hash))
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
