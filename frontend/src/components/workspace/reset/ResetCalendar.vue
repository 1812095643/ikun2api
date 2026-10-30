<script setup lang="ts">
import { computed, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { formatTime, type ResetEvent } from '@/api/workspace'
const props = defineProps<{ events: ResetEvent[]; updatedAt: string }>()
const selected = ref('')
const dateKey = (value: Date) => new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).format(value)
const days = computed(() => {
  const today = dateKey(new Date()), end = new Date(today + 'T12:00:00Z')
  const dates = Array.from({ length: 120 }, (_, index) => {
    const date = new Date(end)
    date.setUTCDate(end.getUTCDate() - 119 + index)
    const key = date.toISOString().slice(0, 10)
    const events = props.events.filter(event => dateKey(new Date(event.resetAt)) === key)
    const kinds = new Set(events.map(event => event.resetType))
    return { key, events, known: key <= dateKey(new Date(props.updatedAt)), kind: kinds.size > 1 ? 30 : kinds.has(10) ? 10 : kinds.has(20) ? 20 : 0 }
  })
  const padding = new Date(dates[0].key + 'T12:00:00Z').getUTCDay()
  const result = [...Array.from({ length: padding }, () => null), ...dates]
  while (result.length % 7) result.push(null)
  return result
})
const selectedDay = computed(() => days.value.find(day => day?.key === selected.value))
</script>

<template>
  <section class="original-calendar-panel">
    <header class="original-calendar-header"><div class="original-calendar-title"><Icon name="calendar" size="sm" /><h2>近 120 天重置</h2></div></header>
    <div class="original-calendar-legend"><small><i data-reset-kind="10" />常规重置</small><small><i data-reset-kind="20" />赠送额度</small><small><i data-reset-kind="30" />同日多种</small></div>
    <div class="original-calendar-calendar">
      <div class="original-calendar-chart">
        <div class="original-calendar-weekdays" aria-hidden="true"><span>一</span><span>三</span><span>五</span></div>
        <div class="original-calendar-grid" :style="{ '--calendar-columns': days.length / 7 }">
          <template v-for="(day, index) in days" :key="day?.key || index">
            <span v-if="!day" class="original-calendar-placeholder" />
            <span v-else class="original-calendar-cell">
              <button class="original-calendar-day" :data-reset="day.kind || undefined" :class="{ 'reset-unsynced-day': !day.known }"
                :aria-label="day.key + '，' + (day.events.length ? day.events.map(event => event.label).join('，') : day.known ? '无已收录记录' : '尚未同步')"
                :aria-pressed="selected === day.key" @click="selected = selected === day.key ? '' : day.key" />
              <span class="original-calendar-tooltip">{{ day.key }} · {{ day.events.length ? day.events.map(event => event.label).join(' / ') : day.known ? '无已收录记录' : '尚未同步' }}</span>
            </span>
          </template>
        </div>
      </div>
    </div>
    <div v-if="selectedDay" class="calendar-selection"><strong>{{ selectedDay.key }}</strong><p v-for="event in selectedDay.events" :key="event.id">{{ formatTime(event.resetAt) }} · {{ event.label }}</p><p v-if="!selectedDay.events.length">{{ selectedDay.known ? '这一天暂无已收录记录' : '这一天的记录尚未同步' }}</p></div>
  </section>
</template>

<style scoped>
.calendar-selection { margin-top: 15px; padding-top: 12px; border-top: 1px solid var(--border); font-size: 11px; color: var(--text); }
.calendar-selection p { margin-top: 5px; color: var(--text-secondary); line-height: 1.6; }
.reset-unsynced-day { opacity: .35; }
</style>
