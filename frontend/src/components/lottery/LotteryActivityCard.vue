<script setup lang="ts">
import { computed } from 'vue'
import { lotteryDrawText, lotteryMoney, statusNames, type LotteryActivity } from '@/api/lottery'
import LotteryRules from './LotteryRules.vue'
import LotteryPrizeList from './LotteryPrizeList.vue'
import Icon from '@/components/icons/Icon.vue'
const props = defineProps<{
  item: LotteryActivity
  now: number
  busy?: boolean
  confirmed?: boolean
  manage?: boolean
}>()
defineEmits<{ join: []; detail: []; edit: [] }>()
const started = computed(() => props.now >= new Date(props.item.spec.start_at).getTime())
const due = computed(
  () =>
    props.item.status === 'open' &&
    ((props.item.spec.draw_at && props.now >= new Date(props.item.spec.draw_at).getTime()) ||
      (props.item.spec.draw_mode !== 'time' && props.item.entry_count >= props.item.spec.capacity))
)
const full = computed(
  () => props.item.spec.capacity > 0 && props.item.entry_count >= props.item.spec.capacity
)
const countdown = computed(() => {
  const target = started.value ? props.item.spec.draw_at : props.item.spec.start_at
  if (!target || props.item.status !== 'open') return ''
  const seconds = Math.max(0, Math.ceil((new Date(target).getTime() - props.now) / 1000))
  if (!seconds) return '开奖中'
  const h = Math.floor(seconds / 3600),
    m = Math.floor((seconds % 3600) / 60),
    s = seconds % 60
  return `${started.value ? '距开奖' : '距报名'} ${h > 24 ? `${Math.floor(h / 24)} 天` : `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`}`
})
const label = computed(() =>
  props.confirmed
    ? '报名成功'
    : props.busy
      ? '正在报名'
      : due.value
        ? '正在开奖'
        : props.item.joined
          ? '已报名 · 等待开奖'
          : !started.value
            ? '即将开始'
            : full.value
              ? '名额已满'
              : !props.item.eligible
                ? '暂未满足条件'
                : '立即报名'
)
</script>
<template>
  <article
    class="lottery-activity"
    :class="{ 'lottery-ticket-confirmed': confirmed, 'lottery-drawing': due }"
  >
    <div class="lottery-activity-top">
      <span class="lottery-kicker">活动 #{{ item.id }}</span
      ><span class="lottery-status" :data-status="item.status">{{
        due ? '开奖中' : statusNames[item.status]
      }}</span>
    </div>
    <button class="lottery-title-button" @click="$emit('detail')">
      <h2>{{ item.spec.title }}</h2>
      <Icon name="externalLink" size="sm" />
    </button>
    <p v-if="item.spec.description" class="lottery-description">{{ item.spec.description }}</p>
    <LotteryPrizeList :prizes="item.spec.prizes" />
    <LotteryRules :spec="item.spec" />
    <div class="lottery-ticket-divider" />
    <div class="lottery-activity-bottom">
      <div>
        <span class="lottery-entry-count"
          ><strong>{{ item.entry_count }}</strong
          >{{ item.spec.capacity ? ` / ${item.spec.capacity} 人` : ' 人已报名' }}</span
        ><small>{{ lotteryDrawText(item.spec) }}</small
        ><span v-if="countdown" class="lottery-countdown">{{ countdown }}</span>
      </div>
      <template v-if="manage"
        ><button
          v-if="item.status === 'draft'"
          class="btn btn-secondary lottery-button lottery-primary"
          @click="$emit('edit')"
        >
          编辑草稿</button
        ><button v-else class="btn btn-secondary lottery-button" @click="$emit('detail')">
          活动记录
        </button></template
      >
      <button
        v-else-if="item.status === 'open'"
        class="btn btn-secondary lottery-button lottery-primary"
        :disabled="busy || item.joined || !item.eligible || !started || full || !!due"
        @click="$emit('join')"
      >
        <Icon v-if="item.joined || confirmed" name="check" size="sm" />{{ label }}
      </button>
      <button v-else class="btn btn-secondary lottery-button" @click="$emit('detail')">
        {{ item.my_win ? `中了 $${lotteryMoney(item.my_win.amount)}` : '查看开奖结果'
        }}<Icon name="arrowRight" size="sm" />
      </button>
    </div>
  </article>
</template>
