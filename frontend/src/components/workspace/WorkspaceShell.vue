<script setup lang="ts">
import { useRoute } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import '@/styles/workspace.css'
defineProps<{ wide?: boolean }>()
const route = useRoute()
</script>

<template>
  <AppLayout class="native-workspace">
    <div
      class="workspace"
      :class="{ 'workspace-wide': wide }"
    >
      <h1 class="mb-4 text-lg font-semibold lg:hidden">{{ route.meta.title }}</h1>
      <slot />
    </div>
  </AppLayout>
</template>
