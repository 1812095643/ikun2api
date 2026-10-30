<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import { formatTime, type ResetSummary } from '@/api/workspace'
const props = defineProps<{ summary: ResetSummary; now: number }>()
const forecast = computed(() => {
  const window = props.summary.announcedWindow
  if (!window) return null
  const start = Date.parse(window.startAt), end = Date.parse(window.endAt), now = props.now
  if (!Number.isFinite(start) || !Number.isFinite(end) || start >= end || now >= end + 86400000) return null
  let headline: string
  if (now >= end) {
    const minutes = Math.floor((now - end) / 60000)
    headline = minutes < 1 ? '已超时不足 1 分钟' : minutes < 60 ? '已超时 ' + minutes + ' 分钟' : '已超时 ' + Math.floor(minutes / 60) + ' 小时 ' + minutes % 60 + ' 分钟'
  } else if (now >= start) headline = '预计 ' + Math.ceil((end - now) / 3600000) + ' 小时内重置'
  else {
    const unit = end - now < 3600000 ? { ms: 60000, label: '分钟' } : end - now <= 259200000 ? { ms: 3600000, label: '小时' } : { ms: 86400000, label: '天' }
    const low = Math.max(0, Math.floor((start - now) / unit.ms)), high = Math.max(1, Math.ceil((end - now) / unit.ms))
    headline = low ? '预计 ' + low + '–' + high + ' ' + unit.label + '后重置' : '预计 ' + high + ' ' + unit.label + '内重置'
  }
  return { headline, overdue: now >= end, range: formatTime(window.startAt) + '–' + formatTime(window.endAt) }
})
const elapsed = computed(() => {
  if (!props.summary.lastResetAt) return '暂无记录'
  const seconds = Math.max(0, (props.now - Date.parse(props.summary.lastResetAt)) / 1000)
  for (const unit of [{ seconds: 31536000, label: '年' }, { seconds: 2592000, label: '个月' }, { seconds: 86400, label: '天' }, { seconds: 3600, label: '小时' }, { seconds: 60, label: '分钟' }])
    if (seconds >= unit.seconds) return Math.max(1, Math.round(seconds / unit.seconds)) + ' ' + unit.label + '前'
  return '刚刚'
})
</script>

<template>
  <section class="original-overview-overview" aria-labelledby="reset-overview-title">
    <header class="original-overview-overviewHeader">
      <div class="original-overview-overviewTitle"><ProviderIcon provider="openai" :size="17" /><h2 id="reset-overview-title">Codex 重置雷达</h2></div>
    </header>
    <div class="original-overview-statuses">
      <article class="original-overview-announcement">
        <div class="original-overview-announcementHeader"><div class="original-overview-statusLabel"><Icon name="bell" size="sm" /><span>重置预告</span></div></div>
        <strong class="original-overview-announcementValue" :class="{ 'original-overview-overdueValue': forecast?.overdue }">{{ forecast?.headline || '暂无新预告' }}</strong>
        <p v-if="forecast" class="original-overview-schedulePill"><Icon name="calendar" size="sm" /><span>{{ forecast.overdue ? '原预计范围' : '预测范围' }} {{ forecast.range }}</span></p>
        <p v-else class="original-overview-statusMeta">尚未发现明确的下一次重置信号</p>
        <p v-if="forecast?.overdue" class="original-overview-statusMeta">等待重置确认</p>
      </article>
      <article class="original-overview-lastReset">
        <div class="original-overview-lastResetLabel"><Icon name="clock" size="sm" /><span>上次重置</span></div>
        <strong class="original-overview-lastResetValue">{{ elapsed }}</strong>
        <p class="original-overview-lastResetMeta">{{ formatTime(summary.lastResetAt) }}</p>
      </article>
    </div>
  </section>
  <dl class="original-overview-stats" aria-label="历史重置统计">
    <div class="original-overview-stat"><dt><Icon name="refresh" size="sm" />总重置次数</dt><dd><strong>{{ summary.stats.totalResets }}</strong><span>次</span></dd></div>
    <div class="original-overview-stat"><dt><Icon name="clock" size="sm" />平均重置间隔</dt><dd><strong>{{ summary.stats.averageResetGapDays }}</strong><span>天</span></dd></div>
    <div class="original-overview-stat"><dt><Icon name="calendar" size="sm" />最长等待天数</dt><dd><strong>{{ summary.stats.longestWaitDays }}</strong><span>天</span></dd></div>
  </dl>
</template>
