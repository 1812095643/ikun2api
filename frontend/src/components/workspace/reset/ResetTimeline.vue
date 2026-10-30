<script setup lang="ts">
import { computed, ref } from 'vue'
import { formatTime, safeExternalURL, type ResetEvent } from '@/api/workspace'
const props = defineProps<{ events: ResetEvent[] }>()
const expanded = ref(false)
const visible = computed(() => (expanded.value ? props.events : props.events.slice(0, 8)))
</script>
<template>
  <section class="ws-panel">
    <h2 class="ws-panel-title">最近重置记录</h2>
    <ol class="reset-timeline">
      <li
        v-for="event in visible"
        :key="event.id"
      >
        <span
          class="timeline-point"
          :class="{ 'credit-point': event.resetType === 20 }"
        />
        <div>
          <time>{{ formatTime(event.resetAt) }}</time>
          <p>{{ event.label }}</p>
          <small v-if="event.resetCreditExpiresAt"
            >有效至 {{ formatTime(event.resetCreditExpiresAt) }}</small
          ><a
            v-if="safeExternalURL(event.sourceUrl)"
            :href="safeExternalURL(event.sourceUrl)"
            target="_blank"
            rel="noopener noreferrer"
            >来源 ↗</a
          >
        </div>
      </li>
    </ol>
    <div
      v-if="events.length > 8"
      class="px-5 pb-5"
    >
      <button
        class="ws-button ws-button-quiet w-full"
        @click="expanded = !expanded"
      >
        {{ expanded ? '收起历史' : `查看全部 ${events.length} 条记录` }}
      </button>
    </div>
  </section>
</template>
<style scoped>
.reset-timeline {
  padding: 22px;
}
.reset-timeline li {
  display: flex;
  gap: 14px;
  position: relative;
  padding-bottom: 22px;
}
.reset-timeline li:last-child {
  padding-bottom: 0;
}
.reset-timeline li:not(:last-child)::before {
  content: '';
  position: absolute;
  left: 4px;
  top: 13px;
  bottom: 0;
  width: 1px;
  background: var(--ws-line);
}
.timeline-point {
  width: 9px;
  height: 9px;
  margin-top: 5px;
  border: 2px solid #14b8a6;
  background: var(--ws-surface);
  border-radius: 50%;
  flex-shrink: 0;
}
.credit-point {
  border-color: #f59e0b;
}
.reset-timeline time {
  font-size: 12px;
  font-weight: 600;
}
.reset-timeline p {
  font-size: 12px;
  color: var(--ws-muted);
  margin-top: 4px;
}
.reset-timeline small {
  display: block;
  font-size: 10px;
  color: var(--ws-muted);
  margin-top: 5px;
}
.reset-timeline a {
  font-size: 11px;
  color: var(--ws-primary);
  display: inline-block;
  margin-top: 5px;
}
</style>
