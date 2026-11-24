<script setup lang="ts">
import MonitorPanel from '@/features/ops-monitor/MonitorPanel.vue'
const incident = new URLSearchParams(location.search).get('monitor_incident') || undefined
</script>
<template>
  <main class="min-h-screen bg-gray-50/70 text-gray-900 dark:bg-dark-950 dark:text-white">
    <MonitorPanel :initial-incident-id="incident" />
  </main>
</template>
