<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import UserPoolPanel from '@/components/pool/UserPoolPanel.vue'
const route=useRoute(),{t}=useI18n()
// Fragment tokens do not enter HTTP URLs, access logs or Referer headers.
const token=computed(()=>new URLSearchParams(route.hash.slice(1)).get('token')||'')
</script>
<template><main class="min-h-screen bg-gray-50 px-4 py-8 dark:bg-dark-950"><div class="mx-auto max-w-4xl"><header class="mb-6"><h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('admin.pool.title') }}</h1></header><UserPoolPanel shared :token="token" /></div></main></template>
