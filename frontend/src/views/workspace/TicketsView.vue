<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import WorkspaceShell from '@/components/workspace/WorkspaceShell.vue'
import TicketWorkspace from '@/components/workspace/tickets/TicketWorkspace.vue'
const route = useRoute()
const admin = computed(() => route.path.startsWith('/admin/'))
</script>
<template>
  <WorkspaceShell class="workspace-tickets"
    ><TicketWorkspace
      :key="String(admin)"
      :admin="admin"
  /></WorkspaceShell>
</template>
