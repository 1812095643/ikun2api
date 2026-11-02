<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { announcementPreview } from '@/utils/announcementContent'
import type { UserAnnouncement } from '@/types'

const props = defineProps<{ items: UserAnnouncement[]; loading: boolean }>()
const emit = defineEmits<{ open: [item: UserAnnouncement] }>()
const { t, locale } = useI18n()
const entries = computed(() => props.items.map(item => {
  const date = new Date(item.starts_at || item.created_at)
  const valid = !Number.isNaN(date.getTime())
  return { item, preview: announcementPreview(item.content),
    month: valid ? date.toLocaleDateString(locale.value, { year: 'numeric', month: 'long' }) : '—',
    day: valid ? date.toLocaleDateString(locale.value, { month: '2-digit', day: '2-digit' }) : '—',
    time: valid ? date.toLocaleTimeString(locale.value, { hour: '2-digit', minute: '2-digit', hour12: false }) : '' }
}))
</script>
<template>
  <div v-if="loading && !items.length" role="status" class="space-y-5 py-6" :aria-label="t('common.loading')">
    <div v-for="n in 3" :key="n" class="h-20 animate-pulse rounded-lg bg-gray-100 motion-reduce:animate-none dark:bg-dark-700/40"></div>
  </div>
  <ol v-else-if="entries.length" class="announcement-timeline">
    <li v-for="(entry, index) in entries" :key="entry.item.id">
      <h3 v-if="index === 0 || entries[index - 1].month !== entry.month" class="timeline-month">{{ entry.month }}</h3>
      <button type="button" class="timeline-entry group" :class="{ 'is-unread': !entry.item.read_at }" :aria-label="entry.item.title + ' · ' + (entry.item.read_at ? t('announcements.read') : t('announcements.unread'))" @click="emit('open', entry.item)">
        <time class="timeline-date" :datetime="entry.item.starts_at || entry.item.created_at"><span>{{ entry.day }}</span><span class="timeline-time">{{ entry.time }}</span></time>
        <span class="timeline-track" aria-hidden="true"><span class="timeline-dot"></span></span>
        <span class="timeline-copy">
          <span class="flex items-start justify-between gap-3"><span class="min-w-0 break-words font-semibold text-gray-900 dark:text-dark-50">{{ entry.item.title }}</span><span v-if="!entry.item.read_at" class="timeline-unread">{{ t('announcements.unread') }}</span></span>
          <span v-if="entry.preview" class="mt-1.5 line-clamp-2 break-words text-sm leading-6 text-gray-500 dark:text-dark-400">{{ entry.preview }}</span>
        </span>
        <Icon name="chevronRight" size="sm" class="mt-1 shrink-0 text-gray-400 transition-transform group-hover:translate-x-0.5 motion-reduce:transform-none" />
      </button>
    </li>
  </ol>
  <div v-else class="py-14 text-center"><Icon name="bell" size="xl" class="mx-auto mb-3 text-gray-300 dark:text-dark-500" /><p class="text-sm text-gray-500 dark:text-dark-400">{{ t('announcements.empty') }}</p></div>
</template>
<style scoped>
.timeline-month { @apply mb-4 mt-6 text-xs font-semibold tracking-wide text-gray-400 dark:text-dark-400; }
.announcement-timeline > li:first-child .timeline-month { @apply mt-0; }
.timeline-entry { @apply relative flex w-full items-start gap-3 rounded-lg pr-2 text-left text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 sm:gap-4; }
.timeline-date { @apply flex w-12 shrink-0 flex-col items-start pt-1 text-xs font-medium tabular-nums text-gray-500 dark:text-dark-300; }
.timeline-time { @apply mt-1 font-normal text-gray-400 dark:text-dark-500; }
.timeline-track { @apply relative flex w-3 self-stretch justify-center; }
.timeline-track::after { content: ''; @apply absolute bottom-0 top-3 w-px bg-gray-200 dark:bg-dark-600; }
.announcement-timeline > li:last-child .timeline-track::after { @apply hidden; }
.timeline-dot { @apply relative z-10 mt-1.5 h-2 w-2 shrink-0 rounded-full bg-gray-300 ring-4 ring-white dark:bg-dark-500 dark:ring-dark-800; }
.is-unread .timeline-dot { @apply bg-primary-500 dark:bg-primary-400; }
.timeline-copy { @apply min-w-0 flex-1 pb-7; }
.timeline-unread { @apply shrink-0 rounded-md bg-primary-50 px-1.5 py-0.5 text-[10px] font-medium text-primary-700 dark:bg-primary-900/25 dark:text-primary-300; }
.timeline-entry:hover .timeline-copy { @apply opacity-80; }
</style>
