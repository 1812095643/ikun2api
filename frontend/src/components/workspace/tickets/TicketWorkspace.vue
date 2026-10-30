<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { useTickets } from '@/composables/workspace/useTickets'
import { workspaceError, type TicketDraft, type TicketImage } from '@/api/workspace'
import WorkspaceDialog from '../WorkspaceDialog.vue'
import TicketList from './TicketList.vue'
import TicketComposer from './TicketComposer.vue'
import TicketConversation from './TicketConversation.vue'
import TicketNotificationDialog from './TicketNotificationDialog.vue'
const props = defineProps<{ admin: boolean }>()
const {
  api,
  filters,
  result,
  detail,
  selected,
  loading,
  detailLoading,
  busy,
  error,
  creating,
  loadList,
  loadDetail,
  select,
  back,
  create,
  reply,
  changeStatus,
  deleteTicket,
  cleanupEnded
} = useTickets(props.admin)
const draft = reactive({ subject: '', category: 'general', priority: 'normal' })
const deleteImage = ref<TicketImage | null>(null)
const notifying = ref(false)
const deleteTarget = ref<{ id: number; subject: string } | null>(null)
const quota = computed(() => result.value?.quota)
const resetsAt = computed(() => quota.value ? new Date(quota.value.resets_at).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', timeZone: 'Asia/Shanghai' }) : '')
async function submitNew(message: TicketDraft) {
  const success = await create({ ...draft, ...message })
  if (success) draft.subject = ''
  return success
}
async function removeImage() {
  if (!deleteImage.value) return
  busy.value = true
  try {
    await api.deleteImage(selected.value, deleteImage.value.id)
    deleteImage.value = null
    await loadDetail(true)
  } catch (e) {
    error.value = workspaceError(e)
  } finally {
    busy.value = false
  }
}
async function confirmDelete() {
  if (!deleteTarget.value) return
  const target = deleteTarget.value
  const deleted = target.id ? await deleteTicket(target.id) : await cleanupEnded()
  if (deleted !== false && deleted !== null) deleteTarget.value = null
}
async function notificationSent(id: number) {
  notifying.value = false
  filters.page = 1
  filters.search = ''
  filters.status = ''
  await select(id)
  await loadList()
}
</script>

<template>
  <div class="ws-toolbar">
    <div>
      <div
        v-if="result"
        class="ticket-stats"
      >
        <span
          ><strong>{{ result.stats.active }}</strong> 进行中</span
        ><span
          ><strong>{{ result.stats.unread }}</strong> 待阅读</span
        ><span v-if="admin"
          >队列共 <strong>{{ result.queue.total }}</strong> 张</span
        >
      </div>
    </div>
    <div class="ws-actions">
      <button v-if="admin" type="button" class="ticket-notification-launch btn btn-primary" :disabled="busy" @click="notifying = true"><Icon name="mail" size="sm" />通知用户</button>
      <button v-if="admin && result?.stats.ended" class="ws-button ws-button-danger" :disabled="busy" @click="deleteTarget = { id: 0, subject: '' }">清理已结束（{{ result.stats.ended }}）</button>
      <button
        class="ws-icon-btn"
        :disabled="loading"
        aria-label="刷新工单"
        @click="loadList()"
      >
        <Icon
          name="refresh"
          size="sm"
          :class="{ 'ws-spin': loading }"
        /></button
      ><button
        v-if="!admin"
        class="ws-button ws-button-primary"
        :disabled="!quota?.can_create || busy"
        @click="creating = true"
      >
        <Icon
          name="plus"
          size="sm"
        />新建工单
      </button>
    </div>
  </div>
  <div v-if="!admin" class="ticket-quota ws-muted" role="status">
    <span>{{ quota && !quota.can_create ? `今日已提交 1 张 · 北京时间 ${resetsAt} 重置` : '每天可新建 1 张工单，相同问题请在原工单内继续沟通。' }}</span>
    <button v-if="quota?.ticket_id" type="button" class="ticket-existing-link" @click="select(quota.ticket_id)">继续沟通</button>
  </div>
  <div
    v-if="error"
    class="ws-error"
    role="alert"
  >
    <span>{{ error }}</span
    ><button @click="error = ''">关闭</button>
  </div>
  <div
    class="ws-panel ticket-layout"
    :class="{ 'has-detail': selected || creating }"
  >
    <aside class="ticket-sidebar">
      <div class="ticket-filters">
        <label class="ticket-search"
          ><Icon
            name="search"
            size="sm" /><input
            v-model="filters.search"
            class="ws-input"
            :placeholder="admin ? '搜索标题、用户邮箱' : '搜索工单标题'"
            aria-label="搜索工单" /></label
        ><select
          v-model="filters.status"
          class="ws-input"
          aria-label="筛选工单状态"
          @change="filters.page = 1"
        >
          <option value="">全部状态</option>
          <option value="open">待处理</option>
          <option value="in_progress">处理中</option>
          <option value="waiting_user">待回复</option>
          <option value="resolved">已解决</option>
          <option value="closed">已关闭</option>
        </select>
      </div>
      <TicketList
        :result="result"
        :selected="creating ? 0 : selected"
        :loading="loading"
        :admin="admin"
        :busy="busy"
        @select="select"
        @page="filters.page = $event"
        @remove="deleteTarget = { id: $event.id, subject: $event.subject }"
      />
    </aside>
    <div class="ticket-main">
      <section
        v-if="creating"
        class="new-ticket"
      >
        <header>
          <button
            class="ws-button ws-button-quiet"
            @click="back"
          >
            <Icon
              name="arrowLeft"
              size="sm"
            />返回
          </button>
          <h2>新建工单</h2>
        </header>
        <label class="ws-field"
          >标题<input
            v-model="draft.subject"
            class="ws-input"
            maxlength="300"
            placeholder="用一句话概括问题"
        /></label>
        <div class="ws-form-grid">
          <label class="ws-field"
            >问题类型<select
              v-model="draft.category"
              class="ws-input"
            >
              <option value="general">一般咨询</option>
              <option value="account">账号问题</option>
              <option value="billing">账单与充值</option>
              <option value="model">模型使用</option>
              <option value="bug">故障反馈</option>
            </select></label
          ><label class="ws-field"
            >优先级<select
              v-model="draft.priority"
              class="ws-input"
            >
              <option value="low">不紧急</option>
              <option value="normal">普通</option>
              <option value="high">较紧急</option>
              <option value="urgent">紧急</option>
            </select></label
          >
        </div>
        <TicketComposer
          :submit="submitNew"
          :busy="busy"
          :disabled="!draft.subject.trim() || !quota?.can_create"
          submit-label="提交工单"
        />
      </section>
      <div
        v-else-if="detailLoading"
        class="ws-empty h-full"
        role="status"
      >
        <Icon
          name="refresh"
          class="ws-spin"
        />
        <p>读取对话中…</p>
      </div>
      <TicketConversation
        v-else-if="detail"
        :ticket="detail"
        :admin="admin"
        :busy="busy"
        :submit="reply"
        @back="back"
        @status="changeStatus"
        @remove-image="deleteImage = $event"
        @remove="deleteTarget = { id: detail.id, subject: detail.subject }"
      />
      <div
        v-else
        class="ws-empty h-full"
      >
        <Icon
          name="chat"
          size="xl"
        />
        <h2>选择工单查看对话</h2>
        <button
          v-if="selected"
          class="ws-button"
          @click="back"
        >
          返回工单列表
        </button>
      </div>
    </div>
  </div>
  <WorkspaceDialog
    :open="!!deleteImage"
    title="删除已阅图片"
    @update:open="deleteImage = null"
    ><p class="text-sm leading-7">
      这张图片的内容会被删除，对话文字和删除标记会保留。请确认已经阅览并不再需要。
    </p>
    <template #footer
      ><button
        class="ws-button"
        @click="deleteImage = null"
      >
        保留图片</button
      ><button
        class="ws-button ws-button-danger"
        :disabled="busy"
        @click="removeImage"
      >
        确认删除
      </button></template
    ></WorkspaceDialog
  >
  <WorkspaceDialog :open="!!deleteTarget" :title="deleteTarget?.id ? '删除工单' : '清理已结束工单'" @update:open="deleteTarget = null">
    <p class="text-sm leading-7">{{ deleteTarget?.id ? `将删除工单 #${deleteTarget.id}「${deleteTarget.subject}」及全部对话和图片，无法恢复。` : `将删除全部 ${result?.stats.ended || 0} 张已解决或已关闭工单及其对话、图片，无法恢复。` }}</p>
    <template #footer><button class="ws-button" :disabled="busy" @click="deleteTarget = null">取消</button><button class="ws-button ws-button-danger" :disabled="busy" @click="confirmDelete">确认删除</button></template>
  </WorkspaceDialog>
  <TicketNotificationDialog v-if="admin && notifying" @close="notifying = false" @sent="notificationSent" />
</template>

<style scoped>
.ticket-notification-launch {
  @apply from-blue-500 to-blue-600 shadow-blue-500/20 hover:from-blue-600 hover:to-blue-700 hover:shadow-blue-500/20 focus:ring-blue-500/50 dark:shadow-blue-500/15;
}
.ticket-quota {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  margin: -8px 0 18px;
}
.ticket-existing-link { color: var(--ws-primary); }
.ticket-existing-link:hover { text-decoration: underline; }
.ticket-stats {
  display: flex;
  gap: 22px;
  margin-top: 10px;
  font-size: 12px;
  color: var(--ws-muted);
}
.ticket-stats strong {
  color: var(--ws-text);
  font-weight: 600;
}
.ticket-layout {
  display: grid;
  grid-template-columns: 320px minmax(0, 1fr);
  height: min(850px, calc(100dvh - 215px));
  min-height: 420px;
}
.ticket-sidebar {
  border-right: 1px solid var(--ws-line);
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.ticket-filters {
  padding: 18px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  border-bottom: 1px solid var(--ws-line);
}
.ticket-search {
  position: relative;
}
.ticket-search > svg {
  position: absolute;
  top: 12px;
  left: 12px;
  color: var(--ws-muted);
}
.ticket-search input {
  padding-left: 36px;
}
.ticket-main {
  min-width: 0;
  min-height: 0;
}
.new-ticket {
  display: flex;
  flex-direction: column;
  gap: 20px;
  padding: 24px;
  height: 100%;
  overflow: auto;
}
.new-ticket header {
  display: flex;
  align-items: center;
  gap: 12px;
}
.new-ticket header h2 {
  font-size: 17px;
  font-weight: 600;
}
@media (max-width: 1100px) {
  .ticket-layout {
    grid-template-columns: 280px minmax(0, 1fr);
  }
}
@media (max-width: 900px) {
  .ticket-layout {
    grid-template-columns: 1fr;
    min-height: 520px;
    height: calc(100dvh - 240px);
  }
  .ticket-main {
    display: none;
  }
  .has-detail .ticket-sidebar {
    display: none;
  }
  .has-detail .ticket-main {
    display: block;
  }
  .ticket-sidebar {
    border-right: none;
  }
}
</style>
