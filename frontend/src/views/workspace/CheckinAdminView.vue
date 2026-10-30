<script setup lang="ts">
import WorkspaceShell from "@/components/workspace/WorkspaceShell.vue";
import CheckinAdminSettings from "@/components/checkin/CheckinAdminSettings.vue";
import BlindboxAdminSettings from "@/components/checkin/BlindboxAdminSettings.vue";
import BlindboxAdminHistory from "@/components/checkin/BlindboxAdminHistory.vue";
</script>

<template>
  <WorkspaceShell
    ><div class="space-y-8">
      <CheckinAdminSettings /><BlindboxAdminSettings /><BlindboxAdminHistory /></div
  ></WorkspaceShell>
</template>
