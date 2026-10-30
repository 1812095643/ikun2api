<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import {
  formatTime,
  type Ticket,
  type TicketDraft,
  type TicketImage,
  type TicketStatus
} from '@/api/workspace'
import TicketComposer from './TicketComposer.vue'
import TicketAttachment from './TicketAttachment.vue'
const props = defineProps<{
  ticket: Ticket
  admin: boolean
  busy: boolean
  submit: (draft: TicketDraft) => Promise<boolean>
}>()
const emit = defineEmits<{
  status: [status: TicketStatus]
  back: []
  removeImage: [image: TicketImage]
  remove: []
}>()
const messages = ref<HTMLElement | null>(null)
const labels: Record<string, string> = {
  open: '待处理',
  in_progress: '处理中',
  waiting_user: '待回复',
  resolved: '已解决',
  closed: '已关闭'
}
watch(
  () => props.ticket.messages?.length,
  async () => {
    await nextTick()
    if (messages.value) messages.value.scrollTop = messages.value.scrollHeight
  },
  { immediate: true }
)
</script>

<template>
  <section
    class="ticket-conversation"
    :aria-label="ticket.subject"
  >
    <header class="conversation-header">
      <div class="conversation-heading">
        <button
          class="ws-icon-btn mobile-back"
          aria-label="返回工单列表"
          @click="emit('back')"
        >
          <Icon
            name="arrowLeft"
            size="sm"
          />
        </button>
        <div>
          <p class="ws-muted">
            {{ ticket.origin === 'admin' ? '管理员通知' : '工单' }} #{{ ticket.id }}<span v-if="admin"> · {{ ticket.user_email }}</span>
          </p>
          <h2>{{ ticket.subject }}</h2>
        </div>
      </div>
      <div class="ws-actions">
        <select
          v-if="admin"
          class="ws-input status-select"
          aria-label="工单状态"
          :value="ticket.status"
          :disabled="busy"
          @change="emit('status', ($event.target as HTMLSelectElement).value as TicketStatus)"
        >
          <option
            v-for="(label, key) in labels"
            :key="key"
            :value="key"
          >
            {{ label }}
          </option></select
        ><template v-else
          ><span class="ws-label ws-label-active">{{ labels[ticket.status] }}</span
          ><button
            class="ws-button ws-button-quiet"
            :disabled="busy"
            @click="emit('status', ticket.status === 'closed' ? 'open' : 'closed')"
          >
            {{ ticket.status === 'closed' ? '重新打开' : '关闭工单' }}
          </button></template
        ><button v-if="admin" class="ws-button ws-button-danger" :disabled="busy" @click="emit('remove')">删除工单</button>
      </div>
    </header>
    <div
      v-if="!admin && ticket.queue_position && !(ticket.origin === 'admin' && ticket.status === 'waiting_user')"
      class="conversation-queue"
    >
      当前位于处理队列第 <strong>{{ ticket.queue_position }}</strong> 位，回复后会在这里通知你。
    </div>
    <div
      ref="messages"
      class="conversation-messages"
      aria-live="polite"
      aria-relevant="additions"
    >
      <article
        v-for="message in ticket.messages"
        :key="message.id"
        :class="[
          'ticket-message',
          {
            'message-event': message.kind === 'event',
            'message-admin': message.author_role === 'admin'
          }
        ]"
      >
        <template v-if="message.kind === 'event'"
          ><span>{{ message.content }}</span
          ><time>{{ formatTime(message.created_at) }}</time></template
        >
        <template v-else
          ><header>
            <span class="message-avatar">{{
              message.author_role === 'admin' ? '服' : (message.author_name || '用').slice(0, 1)
            }}</span
            ><strong>{{
              message.author_role === 'admin' ? '支持团队' : message.author_name || '用户'
            }}</strong
            ><time>{{ formatTime(message.created_at) }}</time>
          </header>
          <div class="message-body">
            <p v-if="message.content">{{ message.content }}</p>
            <div
              v-if="message.images.length"
              class="message-images"
            >
              <TicketAttachment
                v-for="image in message.images"
                :key="image.id"
                :image="image"
                :ticket-id="ticket.id"
                :admin="admin"
                @remove="emit('removeImage', $event)"
              />
            </div></div
        ></template>
      </article>
    </div>
    <div class="conversation-compose">
      <div
        v-if="ticket.status === 'closed' && !admin"
        class="ws-muted text-center"
      >
        这张工单已关闭，需要继续沟通时可点击「重新打开」。
      </div>
      <TicketComposer
        v-else
        :busy="busy"
        :submit="submit"
      />
    </div>
  </section>
</template>

<style scoped>
.ticket-conversation {
  display: flex;
  flex-direction: column;
  min-width: 0;
  height: 100%;
}
.conversation-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  padding: 20px 24px;
  border-bottom: 1px solid var(--ws-line);
}
.conversation-heading {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.conversation-heading h2 {
  font-size: 16px;
  font-weight: 600;
  line-height: 1.6;
  margin-top: 4px;
  overflow-wrap: anywhere;
}
.status-select {
  width: 110px;
}
.conversation-queue {
  padding: 10px 24px;
  font-size: 12px;
  color: var(--ws-primary);
  background: color-mix(in srgb, var(--ws-primary) 5%, var(--ws-surface));
}
.conversation-messages {
  flex: 1;
  overflow-y: auto;
  padding: 24px;
  min-height: 140px;
}
.ticket-message + .ticket-message {
  margin-top: 25px;
}
.ticket-message header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  font-size: 12px;
}
.ticket-message time {
  color: var(--ws-muted);
  font-size: 11px;
  margin-left: auto;
}
.message-avatar {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  background: var(--ws-soft);
  border-radius: 8px;
  font-size: 11px;
}
.message-admin .message-avatar {
  color: var(--ws-primary);
  background: color-mix(in srgb, var(--ws-primary) 10%, var(--ws-surface));
}
.message-body {
  margin-left: 34px;
  padding: 14px 16px;
  border-radius: 0 10px 10px 10px;
  background: var(--ws-soft);
  font-size: 13px;
  line-height: 1.8;
}
.message-body p {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.message-admin .message-body {
  background: color-mix(in srgb, var(--ws-primary) 6%, var(--ws-surface));
}
.message-images {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 10px;
}
.message-event {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--ws-muted);
  font-size: 11px;
}
.message-event time {
  margin-left: 0;
}
.conversation-compose {
  padding: 18px 24px;
  border-top: 1px solid var(--ws-line);
}
.mobile-back {
  display: none;
}
@media (max-width: 900px) {
  .mobile-back {
    display: inline-flex;
  }
  .conversation-header {
    padding: 16px;
    flex-wrap: wrap;
  }
  .conversation-messages,
  .conversation-compose {
    padding: 16px;
  }
}
</style>
