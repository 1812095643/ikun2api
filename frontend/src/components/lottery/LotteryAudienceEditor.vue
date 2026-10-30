<script setup lang="ts">
import { conditionNames, type ConditionKind, type LotteryCondition } from '@/api/lottery'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
const conditions = defineModel<LotteryCondition[]>('conditions', { required: true })
const match = defineModel<'all' | 'any'>('match', { required: true })
const date = defineModel<string>('date', { required: true })
const kinds = Object.entries(conditionNames).map(([value, label]) => ({ value, label }))
const matchOptions = [
  { value: 'all', label: '满足全部条件' },
  { value: 'any', label: '满足任一条件' }
]
function add(kind: ConditionKind) {
  conditions.value = [
    ...conditions.value,
    { kind, value: kind.includes('registered') ? date.value : kind === 'user_ids' ? '' : '0' }
  ]
}
function change(index: number, key: 'kind' | 'value', value: string) {
  conditions.value = conditions.value.map((c, i) =>
    i === index
      ? {
          ...c,
          [key]: value,
          ...(key === 'kind'
            ? { value: value.includes('registered') ? date.value : value === 'user_ids' ? '' : '0' }
            : {})
        }
      : c
  )
}
</script>
<template>
  <section class="lottery-form-section">
    <header>
      <h3>参与条件</h3>
    </header>
    <div class="lottery-preset-row">
      <button type="button" :class="{ active: !conditions.length }" @click="conditions = []">
        全部用户</button
      ><button type="button" @click="add('spend_today')">当日消费</button
      ><button type="button" @click="add('recharge_today')">当日充值</button
      ><button type="button" @click="add('registered_before')">老用户</button
      ><button type="button" @click="add('user_ids')">指定用户</button>
    </div>
    <template v-if="conditions.length"
      ><div class="lottery-form-inline">
        <label
          >条件关系<Select v-model="match" :options="matchOptions" aria-label="条件关系" /></label
        ><label
          >统计日期（北京时间）<input v-model="date" class="input" type="date" required
        /></label>
      </div>
      <div class="lottery-condition-editor">
        <div v-for="(condition, index) in conditions" :key="index">
          <Select
            :model-value="condition.kind"
            :options="kinds"
            :aria-label="`第 ${index + 1} 个条件类型`"
            @update:model-value="change(index, 'kind', String($event))"
          /><input
            class="input"
            :value="condition.value"
            :type="
              condition.kind.includes('registered')
                ? 'date'
                : condition.kind === 'user_ids'
                  ? 'text'
                  : 'number'
            "
            :aria-label="`第 ${index + 1} 个条件值`"
            :placeholder="condition.kind === 'user_ids' ? '例如：12,34,56' : '0'"
            min="0"
            step="0.00000001"
            required
            @input="change(index, 'value', ($event.target as HTMLInputElement).value)"
          /><button
            class="lottery-icon-button"
            type="button"
            :aria-label="`删除条件 ${index + 1}`"
            @click="conditions = conditions.filter((_, i) => i !== index)"
          >
            <Icon name="x" size="sm" />
          </button>
        </div>
      </div>
      <button
        v-if="conditions.length < 10"
        class="lottery-text-button"
        type="button"
        @click="add('spend_total')"
      >
        + 添加条件
      </button></template
    >
  </section>
</template>
