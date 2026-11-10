<script setup lang="ts">
import { historyFilterOptions, type LotteryHistoryFilter } from '@/api/lottery'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
defineProps<{ manage: boolean; compact?: boolean; fixedActivity?: boolean; loading: boolean }>()
const result = defineModel<LotteryHistoryFilter>('result', { required: true })
const activity = defineModel<string>('activity', { required: true })
const search = defineModel<string>('search', { required: true })
defineEmits<{ apply: []; refresh: [] }>()
</script>
<template>
  <form class="lottery-history-filters" @submit.prevent="$emit('apply')">
    <label
      ><span :class="{ 'sr-only': compact }">参与结果</span><Select v-model="result" :options="historyFilterOptions" aria-label="参与结果"
    /></label>
    <label v-if="!fixedActivity"
      ><span :class="{ 'sr-only': compact }">活动</span><input
        v-model="activity"
        class="input"
        type="search"
        maxlength="100"
        aria-label="活动名称或编号"
        placeholder="活动名称或编号"
    /></label>
    <label v-if="manage"
      ><span :class="{ 'sr-only': compact }">用户</span><input
        v-model="search"
        class="input"
        type="search"
        maxlength="100"
        aria-label="邮箱、用户名或用户编号"
        placeholder="邮箱、用户名或用户编号"
    /></label>
    <button class="btn btn-secondary lottery-button" type="submit" :disabled="loading">
      查询记录
    </button>
    <button
      class="lottery-icon-button"
      type="button"
      aria-label="刷新参与记录"
      :disabled="loading"
      @click="$emit('refresh')"
    >
      <Icon name="refresh" size="sm" />
    </button>
  </form>
</template>
