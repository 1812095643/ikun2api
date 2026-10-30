<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import { formatTime, type TicketPage } from '@/api/workspace'
defineProps<{ result: TicketPage | null; selected: number; loading: boolean; admin: boolean; busy: boolean }>()
const emit = defineEmits<{ select: [id: number]; page: [page: number]; remove: [ticket: TicketPage['items'][number]] }>()
const labels: Record<string, string> = {
  open: '待处理',
  in_progress: '处理中',
  waiting_user: '待回复',
  resolved: '已解决',
  closed: '已关闭'
}
</script>

<template>
  <div class="ticket-list">
    <div
      v-if="loading && !result"
      class="ws-empty"
      role="status"
    >
      <Icon
        name="refresh"
        class="ws-spin"
      />
      <p>读取工单中…</p>
    </div>
    <div
      v-else-if="!result?.items.length"
      class="ws-empty"
    >
      <Icon
        name="chat"
        size="lg"
      />
      <h2>暂无工单</h2>
    </div>
    <div
      v-for="ticket in result?.items"
      :key="ticket.id"
      class="ws-ticket-row"
      :class="{ selected: selected === ticket.id }"
    >
      <button type="button" class="ticket-row-select" :aria-pressed="selected === ticket.id" @click="emit('select', ticket.id)">
      <div class="ticket-row-meta">
        <span>#{{ ticket.id }}<span v-if="ticket.origin === 'admin'" class="ticket-origin">管理员通知</span></span
        ><span>{{ formatTime(ticket.updated_at) }}</span>
      </div>
      <div class="ticket-row-title">
        <strong>{{ ticket.subject }}</strong
        ><span
          v-if="ticket.unread_count"
          class="unread-dot"
          :aria-label="`${ticket.unread_count}条未读`"
        />
      </div>
      <div class="ticket-row-footer">
        <span
          class="ws-label"
          :class="{ 'ws-label-active': !['closed', 'resolved'].includes(ticket.status) }"
          >{{ labels[ticket.status] }}</span
        ><span
          v-if="admin"
          class="truncate ws-muted"
          >{{ ticket.user_email }}</span
        ><span
          v-else-if="ticket.queue_position && !(ticket.origin === 'admin' && ticket.status === 'waiting_user')"
          class="ws-muted"
          >队列第 {{ ticket.queue_position }} 位</span
        ><span
          v-if="ticket.priority === 'urgent'"
          class="urgent"
          >紧急</span
        >
      </div>
      </button>
      <button v-if="admin" type="button" class="ticket-row-delete" :disabled="busy" :aria-label="`删除工单 #${ticket.id}`" @click="emit('remove', ticket)">删除</button>
    </div>
    <div
      v-if="result && result.total > result.page_size"
      class="ws-pagination"
    >
      <span>共 {{ result.total }} 张</span>
      <div class="ws-actions">
        <button
          class="ws-icon-btn"
          :disabled="result.page <= 1"
          aria-label="上一页"
          @click="emit('page', result.page - 1)"
        >
          <Icon
            name="chevronLeft"
            size="sm"
          /></button
        ><span>{{ result.page }}</span
        ><button
          class="ws-icon-btn"
          :disabled="result.page * result.page_size >= result.total"
          aria-label="下一页"
          @click="emit('page', result.page + 1)"
        >
          <Icon
            name="chevronRight"
            size="sm"
          />
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ticket-origin { margin-left: 8px; color: var(--ws-primary); }
.ticket-list {
  overflow-y: auto;
  flex: 1;
  min-height: 0;
}
.ws-ticket-row {
  position: relative;
  display: block;
  width: 100%;
  text-align: left;
  border-bottom: 1px solid var(--ws-line);
  transition: background 0.15s;
}
.ticket-row-select {
  display: block;
  width: 100%;
  padding: 18px 20px;
  text-align: left;
}
.ticket-row-delete {
  position: absolute;
  right: 12px;
  bottom: 8px;
  padding: 10px 4px;
  color: #dc2626;
  font-size: 11px;
}
.ticket-row-delete:hover { text-decoration: underline; }
.ticket-row-delete:disabled { opacity: 0.45; }
.dark .ticket-row-delete { color: #f87171; }
.ws-ticket-row:hover {
  background: var(--ws-soft);
}
.ws-ticket-row.selected {
  background: color-mix(in srgb, var(--ws-primary) 7%, var(--ws-surface));
  box-shadow: inset 3px 0 var(--ws-primary);
}
.ticket-row-meta,
.ticket-row-footer,
.ticket-row-title {
  display: flex;
  align-items: center;
  gap: 10px;
}
.ticket-row-meta {
  justify-content: space-between;
  color: var(--ws-muted);
  font-size: 11px;
}
.ticket-row-title {
  margin: 9px 0 13px;
}
.ticket-row-title strong {
  font-size: 14px;
  font-weight: 600;
  line-height: 1.55;
  overflow-wrap: anywhere;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  flex: 1;
}
.unread-dot {
  width: 7px;
  height: 7px;
  border-radius: 100%;
  background: #2563eb;
  flex-shrink: 0;
}
.ticket-row-footer {
  gap: 8px;
  padding-right: 34px;
}
.urgent {
  color: #ea580c;
  font-size: 11px;
  margin-left: auto;
}
</style>
