<script setup lang="ts">
import { shallowRef, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import LotteryOverview from './LotteryOverview.vue'
import LotteryHistory from './LotteryHistory.vue'
import { lotteryTime, type LotteryActivity } from '@/api/lottery'
const props = defineProps<{ item: LotteryActivity; busy: boolean; loading?: boolean }>()
const emit = defineEmits<{ cancel: []; publish: []; edit: [item: LotteryActivity]; refresh: [] }>()
const tab = shallowRef<'entries' | 'audit'>('entries')
const confirming = shallowRef<'publish' | 'cancel' | null>(null)
const revision = shallowRef(0)
watch(() => [props.item.id, props.item.revision, props.item.entry_count], () => { revision.value++; confirming.value = null })
const eventNames: Record<string, string> = { save: '保存规则', update: '调整活动', publish: '发布活动', cancel: '取消活动', join: '用户报名', draw: '系统开奖', claim: '奖金入账', amend_conditions: '调整参与条件', amend_draw_schedule: '调整开奖时间' }
function confirm() {
  if (props.busy || !confirming.value) return
  if (confirming.value === 'publish') emit('publish')
  else emit('cancel')
  confirming.value = null
}
</script>
<template>
  <div class="lottery-admin-detail">
    <LotteryOverview :item="item" />
    <div class="lottery-detail-nav">
      <nav class="lottery-tabs" aria-label="活动明细">
        <button type="button" :class="{ active: tab === 'entries' }" :aria-pressed="tab === 'entries'" @click="tab = 'entries'">参与记录 <span>{{ item.entry_count }}</span></button>
        <button type="button" :class="{ active: tab === 'audit' }" :aria-pressed="tab === 'audit'" @click="tab = 'audit'">操作记录</button>
      </nav>
      <div class="lottery-actions">
        <button v-if="['draft', 'open'].includes(item.status)" type="button" class="btn btn-secondary btn-sm" :disabled="busy || loading" @click="emit('edit', item)"><Icon name="edit" size="sm" />{{ item.status === 'open' ? '调整活动' : '编辑草稿' }}</button>
        <button v-if="item.status === 'draft'" type="button" class="btn btn-primary btn-sm" :disabled="busy || loading" @click="confirming = 'publish'">发布</button>
        <button v-if="item.status === 'open'" type="button" class="lottery-text-button lottery-cancel-action" :disabled="busy || loading" @click="confirming = 'cancel'">取消活动</button>
        <button type="button" class="lottery-icon-button" aria-label="刷新活动详情" :disabled="busy || loading" @click="emit('refresh')"><Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" /></button>
      </div>
    </div>
    <LotteryHistory v-if="tab === 'entries'" manage compact :lottery-id="item.id" :revision="revision" />
    <section v-else class="lottery-audit-panel" aria-label="操作记录">
      <div v-if="item.result_digest" class="lottery-digest">开奖凭证 <code>{{ item.result_digest }}</code></div>
      <ol class="lottery-audit-events">
        <li v-for="event in item.events" :key="event.id"><time>{{ lotteryTime(event.created_at) }}</time><strong>{{ eventNames[event.action] || event.action }}</strong><span>{{ event.actor_id ? `#${event.actor_id}` : '系统' }}</span></li>
      </ol>
      <p v-if="!item.events?.length" class="lottery-muted">暂无操作记录</p>
    </section>
    <BaseDialog :show="!!confirming" :title="confirming === 'publish' ? '发布抽奖' : '取消本场活动'" width="narrow" :z-index="70" :show-close-button="!busy" :close-on-escape="!busy" @close="confirming = null">
      <p class="text-sm leading-6 text-gray-600 dark:text-gray-300">{{ confirming === 'publish' ? '确认按当前奖项和规则开放报名？' : `已有 ${item.entry_count} 人报名，取消后本场不再开奖。` }}</p>
      <template #footer><button type="button" class="btn btn-secondary" :disabled="busy" @click="confirming = null">返回</button><button type="button" class="btn" :class="confirming === 'cancel' ? 'btn-danger' : 'btn-primary'" :disabled="busy" @click="confirm">{{ confirming === 'publish' ? '确认发布' : '确认取消' }}</button></template>
    </BaseDialog>
  </div>
</template>
