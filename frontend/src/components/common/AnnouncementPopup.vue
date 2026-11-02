<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAnnouncementStore } from '@/stores/announcements'
import { formatRelativeWithDateTime } from '@/utils/format'
import type { Announcement, UserAnnouncement } from '@/types'
import BaseDialog from './BaseDialog.vue'
import AnnouncementContent from './AnnouncementContent.vue'
import Icon from '@/components/icons/Icon.vue'
type PreviewAnnouncement = Pick<Announcement | UserAnnouncement, 'title' | 'content' | 'created_at'>
const props = withDefaults(defineProps<{ announcement?: PreviewAnnouncement | null; preview?: boolean }>(), { announcement: null, preview: false })
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const store = useAnnouncementStore()
const busy = shallowRef(false)
const displayedAnnouncement = computed(() => props.preview ? props.announcement : store.currentPopup)
async function handleDismiss() {
  if (props.preview) { emit('close'); return }
  if (busy.value) return
  busy.value = true
  try { await store.dismissPopup() } finally { busy.value = false }
}
</script>
<template>
  <BaseDialog :show="!!displayedAnnouncement" :title="t('announcements.title')" width="wide" :z-index="120" content-class="announcement-popup-dialog" :show-close-button="preview" :close-on-escape="preview" @close="handleDismiss">
    <template v-if="displayedAnnouncement">
      <div class="mb-6 border-b border-gray-100 pb-5 dark:border-dark-700">
        <div class="mb-3 flex flex-wrap items-center gap-3 text-xs text-gray-400 dark:text-dark-400"><span v-if="!preview" class="rounded-md bg-primary-50 px-2 py-1 font-medium text-primary-700 dark:bg-primary-900/20 dark:text-primary-300">{{ t('announcements.unread') }}</span><time>{{ formatRelativeWithDateTime(displayedAnnouncement.created_at) }}</time></div>
        <h2 class="break-words text-xl font-semibold leading-8 tracking-tight text-gray-900 dark:text-dark-50">{{ displayedAnnouncement.title }}</h2>
      </div>
      <AnnouncementContent :content="displayedAnnouncement.content" />
    </template>
    <template #footer><button type="button" data-testid="announcement-popup-dismiss" class="btn btn-primary ml-auto" :disabled="busy" @click="handleDismiss"><Icon v-if="!preview" name="check" size="sm" />{{ preview ? t('common.close') : t('announcements.markRead') }}</button></template>
  </BaseDialog>
</template>
<style>
.announcement-popup-dialog { width: 100%; max-width: 44rem !important; }
@media (prefers-reduced-motion: reduce) {
  .announcement-popup-dialog, .announcement-dialog { transition: none !important; animation: none !important; }
}
</style>
