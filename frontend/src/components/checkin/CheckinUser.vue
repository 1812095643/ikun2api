<script setup lang="ts">
import CheckinClaimPanel from "./CheckinClaimPanel.vue";
import CheckinRecords from "./CheckinRecords.vue";
import BlindboxSection from "./BlindboxSection.vue";
import { useCheckin } from "@/composables/workspace/useCheckin";

const { status, loading, busy, error, feedback, load, claim, changeMonth } =
  useCheckin();
</script>

<template>
  <section class="space-y-6">
    <p
      v-if="error"
      class="rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300"
      role="alert"
    >
      {{ error }}
    </p>
    <Transition name="checkin-feedback">
      <p
        v-if="feedback"
        class="rounded-lg bg-blue-50 px-4 py-3 text-sm text-blue-700 dark:bg-blue-950/40 dark:text-blue-300"
        role="status"
      >
        {{ feedback }}
      </p>
    </Transition>
    <template v-if="status">
      <CheckinClaimPanel
        :status="status"
        :busy="busy"
        :loading="loading"
        @claim="claim"
        @month="changeMonth"
      />
      <CheckinRecords
        :status="status"
        :loading="loading"
        :busy="busy"
        @page="load"
        @refresh="load()"
      />
    </template>
    <BlindboxSection
      v-if="status"
      :checkin-revision="`${status.today}-${status.checked_in}`"
    />
    <p
      v-else-if="loading"
      class="py-12 text-center text-sm text-gray-500 dark:text-gray-400"
      role="status"
    >
      正在读取签到信息…
    </p>
    <button v-else type="button" class="btn btn-secondary" @click="load()">
      重新加载
    </button>
  </section>
</template>

<style scoped>
.checkin-feedback-enter-active {
  transition:
    opacity 180ms ease-out,
    transform 180ms ease-out;
}
.checkin-feedback-enter-from {
  opacity: 0;
  transform: translateY(-4px);
}
@media (prefers-reduced-motion: reduce) {
  .checkin-feedback-enter-active {
    transition: none;
  }
}
</style>
