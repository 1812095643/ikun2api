<script setup lang="ts">
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import TicketComposer from './TicketComposer.vue'
import { useTicketNotification } from '@/composables/workspace/useTicketNotification'
import type { TicketDraft } from '@/api/workspace'

const emit = defineEmits<{ close: []; sent: [id: number] }>()
const { recipient, options, searching, busy, error, searchError, subject, uncertain, search, select, submit } = useTicketNotification()

async function send(message: TicketDraft) {
  const id = await submit(message)
  if (!id) return false
  emit('sent', id)
  return true
}
</script>

<template>
  <BaseDialog :show="true" title="通知用户" width="normal" body-class="workspace-tickets"
    :show-close-button="!busy && !uncertain" :close-on-escape="!busy && !uncertain" @close="emit('close')">
    <div class="workspace ticket-notification-content">
      <div class="notification-field">
        <label class="input-label" for="ticket-notification-recipient">收件用户</label>
        <Select id="ticket-notification-recipient" :model-value="recipient?.id" :options="options"
          remote clearable :loading="searching" :disabled="busy || uncertain" aria-label="收件用户"
          placeholder="选择用户" search-placeholder="输入用户 ID、邮箱或用户名" empty-text="输入关键字查找用户"
          @search="search" @update:model-value="select" />
        <p v-if="searchError" class="input-error-text" role="alert">{{ searchError }}</p>
      </div>
      <div class="notification-field">
        <label class="input-label" for="ticket-notification-subject">通知标题</label>
        <input id="ticket-notification-subject" v-model="subject" class="input" maxlength="300"
          :disabled="busy || uncertain" placeholder="例如：请检查 API 请求地址" />
      </div>
      <p v-if="error" class="notification-error" role="alert">{{ error }}</p>
      <TicketComposer :submit="send" :busy="busy" :disabled="!recipient || !subject.trim()"
        :locked="uncertain" message-id="ticket-notification-message"
        placeholder="写下需要用户检查的问题和处理方法…" :submit-label="uncertain ? '重试发送' : '发送通知'" />
    </div>
  </BaseDialog>
</template>

<style scoped>
.ticket-notification-content { display: grid; gap: 20px; }
.notification-field { min-width: 0; }
.notification-error { color: #dc2626; font-size: 13px; line-height: 1.6; overflow-wrap: anywhere; }
.dark .notification-error { color: #f87171; }
.ticket-notification-content .input { @apply focus:border-blue-500 focus:ring-blue-500/20; }
.ticket-notification-content :deep(.select-trigger:focus),
.ticket-notification-content :deep(.select-trigger-open) { @apply border-blue-500 ring-blue-500/20; }
</style>

<style>
body:has(.ticket-notification-content) .select-dropdown-portal .select-option-selected {
  @apply bg-blue-50 text-blue-700 dark:bg-blue-900/20 dark:text-blue-300;
}
body:has(.ticket-notification-content) .select-dropdown-portal .select-option-selected > svg { @apply text-blue-500; }
</style>
