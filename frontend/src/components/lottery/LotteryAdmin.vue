<script setup lang="ts">
import { ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import WorkspaceDialog from '@/components/workspace/WorkspaceDialog.vue'
import LotteryEditor from './LotteryEditor.vue'
import LotteryActivityCard from './LotteryActivityCard.vue'
import LotteryAdminDetail from './LotteryAdminDetail.vue'
import LotteryHistory from './LotteryHistory.vue'
import { useLotteryAdmin } from '@/composables/workspace/useLotteryAdmin'
const {
  items,
  total,
  page,
  filter,
  loading,
  busy,
  error,
  editorOpen,
  editing,
  selected,
  message,
  load,
  create,
  edit,
  detail,
  save,
  transition
} = useLotteryAdmin()
const section = ref<'activities' | 'history'>('activities')
function changeFilter(value: string) {
  filter.value = value
  page.value = 1
  void load()
}
function changePage(delta: number) {
  page.value += delta
  void load()
}
</script>
<template>
  <section class="lottery-area lottery-admin">
    <p v-if="error && !editorOpen" class="lottery-error" role="alert">{{ error }}</p>
    <p v-if="message" class="lottery-feedback-text" role="status">{{ message }}</p>
    <LotteryEditor
      v-if="editorOpen"
      :activity="editing"
      :busy="busy"
      :error="error"
      @save="save"
      @close="editorOpen = false"
    />
      <nav class="lottery-tabs lottery-admin-tabs" aria-label="抽奖管理视图">
        <button :class="{ active: section === 'activities' }" @click="section = 'activities'">
          活动管理</button
        ><button :class="{ active: section === 'history' }" @click="section = 'history'">
          全部参与记录
        </button>
      </nav>
      <LotteryHistory v-if="section === 'history'" manage @detail="detail" />
      <template v-else>
        <div class="lottery-filters">
          <button
            v-for="option in [
              { value: '', label: '全部活动' },
              { value: 'draft', label: '草稿' },
              { value: 'open', label: '报名中' },
              { value: 'drawn', label: '已开奖' },
              { value: 'cancelled', label: '已取消' }
            ]"
            :key="option.value"
            :class="{ active: filter === option.value }"
            @click="changeFilter(option.value)"
          >
            {{ option.label }}
          </button>
          <div class="lottery-actions lottery-toolbar-actions">
            <small>{{ total }} 场</small>
            <button
              class="btn btn-primary"
              aria-label="刷新抽奖管理"
              :disabled="loading"
              @click="load"
            >
              <Icon name="refresh" size="sm" />{{ loading ? '刷新中…' : '刷新' }}
            </button>
            <button class="btn btn-primary" @click="create">
              <Icon name="plus" size="sm" />发起抽奖
            </button>
          </div>
        </div>
        <div v-if="!items.length" class="lottery-empty">
          <p>{{ loading ? '加载中…' : '暂无抽奖活动' }}</p>
        </div>
        <div v-else class="lottery-activity-grid">
          <LotteryActivityCard
            v-for="item in items"
            :key="item.id"
            :item="item"
            :now="Date.now()"
            manage
            @detail="detail(item.id)"
            @edit="edit(item)"
          />
        </div>
        <div v-if="total > 12" class="lottery-pagination">
          <button
            class="btn btn-secondary lottery-button"
            :disabled="page <= 1"
            @click="changePage(-1)"
          >
            上一页</button
          ><span>{{ page }} / {{ Math.ceil(total / 12) }}</span
          ><button
            class="btn btn-secondary lottery-button"
            :disabled="page * 12 >= total"
            @click="changePage(1)"
          >
            下一页
          </button>
        </div></template>
    <WorkspaceDialog
      :open="!!selected"
      :title="selected?.spec.title || '活动记录'"
      wide
      @update:open="selected = null"
      ><p v-if="error" class="lottery-error" role="alert">{{ error }}</p>
      <LotteryAdminDetail
        v-if="selected"
        :item="selected"
        :busy="busy"
        @publish="transition('publish')"
        @cancel="transition('cancel')"
    /></WorkspaceDialog>
  </section>
</template>
