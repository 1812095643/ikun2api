<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import { useAuthStore } from '@/stores/auth'
import SystemUpdateDialog from './SystemUpdateDialog.vue'
defineProps<{ version?: string }>()
const auth = useAuthStore()
const admin = computed(() => auth.isAdmin)
const open = shallowRef(false)
</script>

<template>
  <button v-if="admin && version" type="button" class="rounded-lg bg-gray-100 px-2 py-1 text-xs font-medium text-gray-600 transition-colors hover:bg-gray-200 dark:bg-dark-800 dark:text-dark-400 dark:hover:bg-dark-700" aria-label="版本与更新" @click="open = true">v{{ version }}</button>
  <span v-else-if="version" class="rounded-lg bg-gray-100 px-2 py-1 text-xs font-medium text-gray-600 dark:bg-dark-800 dark:text-dark-400" aria-label="Version">v{{ version }}</span>
  <SystemUpdateDialog v-if="admin" :show="open" :version="version" @close="open = false" />
</template>
