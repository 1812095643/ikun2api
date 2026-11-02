<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { storeToRefs } from 'pinia'
import { useAppStore } from '@/stores/app'
import { useAnnouncementStore } from '@/stores/announcements'
import { formatRelativeWithDateTime } from '@/utils/format'
import type { UserAnnouncement } from '@/types'
import Icon from '@/components/icons/Icon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import AnnouncementTimeline from './AnnouncementTimeline.vue'
import AnnouncementContent from './AnnouncementContent.vue'

const { t } = useI18n()
const appStore = useAppStore()
const store = useAnnouncementStore()
const { announcements, loading } = storeToRefs(store)
const isModalOpen = shallowRef(false)
const selectedID = shallowRef<number | null>(null)
const unreadOnly = shallowRef(false)
const marking = shallowRef(false)
const selected = computed(() => announcements.value.find(a => a.id === selectedID.value) || null)
const items = computed(() => unreadOnly.value ? announcements.value.filter(a => !a.read_at) : announcements.value)
function openModal() { selectedID.value = null; isModalOpen.value = true; void store.fetchAnnouncements(true) }
function closeModal() { isModalOpen.value = false; selectedID.value = null }
async function openDetail(item: UserAnnouncement) {
  selectedID.value = item.id
  if (!item.read_at) await store.markAsRead(item.id)
}
async function markAll() {
  marking.value = true
  try { await store.markAllAsRead(); appStore.showSuccess(t('announcements.allMarkedAsRead')) }
  catch (e) { appStore.showError((e as Error).message || t('common.unknownError')) }
  finally { marking.value = false }
}
onMounted(() => {
  window.addEventListener('ikun-open-announcements', openModal)
  window.addEventListener('ikun-tour-close-announcements', closeModal)
})
onBeforeUnmount(() => {
  window.removeEventListener('ikun-open-announcements', openModal)
  window.removeEventListener('ikun-tour-close-announcements', closeModal)
})
</script>
<template>
  <button type="button" class="relative flex h-9 w-9 items-center justify-center rounded-lg text-gray-600 transition-colors hover:bg-gray-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-dark-300 dark:hover:bg-dark-800" :aria-label="t('announcements.title')" @click="openModal">
    <Icon name="bell" size="md" /><span v-if="store.unreadCount" class="absolute -right-1 -top-1 min-w-4 rounded-full bg-red-500 px-1 text-center text-[10px] leading-4 text-white">{{ store.unreadCount > 99 ? '99+' : store.unreadCount }}</span>
  </button>
  <BaseDialog :show="isModalOpen" :title="t('announcements.title')" width="wide" :z-index="100" content-class="announcement-dialog" :body-class="selected ? '' : '!pb-2'" :close-on-click-outside="true" @close="closeModal">
    <div data-tour="announcements-panel">
      <template v-if="selected">
        <button type="button" class="mb-5 inline-flex items-center gap-1 text-xs text-gray-500 hover:text-primary-600 dark:text-dark-400" @click="selectedID = null"><Icon name="chevronLeft" size="sm" />{{ t('announcements.timeline.back') }}</button>
        <div class="mb-6 border-b border-gray-100 pb-5 dark:border-dark-700">
          <div class="mb-2 flex flex-wrap items-center gap-3 text-xs text-gray-400 dark:text-dark-400"><time>{{ formatRelativeWithDateTime(selected.starts_at || selected.created_at) }}</time><span>{{ selected.read_at ? t('announcements.read') : t('announcements.unread') }}</span></div>
          <h2 class="break-words text-xl font-semibold leading-8 tracking-tight text-gray-900 dark:text-dark-50">{{ selected.title }}</h2>
        </div>
        <AnnouncementContent :content="selected.content" />
      </template>
      <template v-else>
        <div class="mb-6 flex flex-wrap items-center justify-between gap-3">
          <nav class="flex items-center rounded-lg bg-gray-100 p-1 dark:bg-dark-900/60" :aria-label="t('announcements.timeline.filter')">
            <button v-for="entry in [{ value: false, label: t('announcements.timeline.all') }, { value: true, label: t('announcements.unread') }]" :key="String(entry.value)" type="button" class="rounded-md px-3 py-1.5 text-xs font-medium transition-colors" :class="unreadOnly === entry.value ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-dark-50' : 'text-gray-500 dark:text-dark-400'" :aria-pressed="unreadOnly === entry.value" @click="unreadOnly = entry.value">{{ entry.label }}<span v-if="entry.value && store.unreadCount" class="ml-1.5 tabular-nums">{{ store.unreadCount }}</span></button>
          </nav>
          <button type="button" class="btn btn-ghost btn-sm" :disabled="!store.unreadCount || marking || loading" @click="markAll"><Icon name="check" size="sm" />{{ t('announcements.markAllRead') }}</button>
        </div>
        <AnnouncementTimeline :items="items" :loading="loading" @open="openDetail" />
      </template>
    </div>
  </BaseDialog>
</template>
<style>
.announcement-dialog { width: 100%; max-width: 46rem !important; }
</style>
