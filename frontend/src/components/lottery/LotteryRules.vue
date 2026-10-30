<script setup lang="ts">
import { lotteryConditionText, type LotterySpec } from '@/api/lottery'
defineProps<{ spec: LotterySpec }>()
</script>
<template>
  <div class="lottery-rules">
    <span v-if="!spec.conditions.length" class="lottery-condition">全部正常用户可参与</span>
    <template v-else>
      <span class="lottery-muted">{{ spec.match === 'all' ? '满足全部' : '满足任一' }}</span>
      <span v-for="(condition, i) in spec.conditions" :key="i" class="lottery-condition">{{
        lotteryConditionText(condition)
      }}</span>
    </template>
    <small v-if="spec.conditions.some((c) => c.kind.endsWith('_today'))" class="lottery-muted"
      >统计日期 {{ spec.business_date }} · 北京时间</small
    >
  </div>
</template>
