<script setup lang="ts">
import { computed } from 'vue'
import { lotteryMoney, prizeNames, type LotteryPrize, type PrizeKey } from '@/api/lottery'
const prizes = defineModel<LotteryPrize[]>({ required: true })
const keys = Object.keys(prizeNames) as PrizeKey[]
const total = computed(() =>
  prizes.value.reduce((sum, p) => sum + Number(p.amount || 0) * p.count, 0)
)
const count = computed(() => prizes.value.reduce((sum, p) => sum + Number(p.count || 0), 0))
function toggle(key: PrizeKey, enabled: boolean) {
  prizes.value = enabled
    ? [...prizes.value, { key, amount: '1', count: 1 }]
    : prizes.value.filter((p) => p.key !== key)
}
function change(key: PrizeKey, field: 'amount' | 'count', value: string) {
  prizes.value = prizes.value.map((p) =>
    p.key === key ? { ...p, [field]: field === 'count' ? Number(value) : value } : p
  )
}
function prize(key: PrizeKey) {
  return prizes.value.find((p) => p.key === key)
}
</script>
<template>
  <section class="lottery-form-section">
    <header>
      <h3>奖项设置</h3>
    </header>
    <div class="lottery-prize-editor">
      <div class="lottery-prize-editor-heading">
        <span>奖项</span><span>每人金额（$）</span><span>中奖人数</span>
      </div>
      <div v-for="key in keys" :key="key" :class="{ 'lottery-prize-disabled': !prize(key) }">
        <label
          ><input
            type="checkbox"
            :checked="!!prize(key)"
            @change="toggle(key, ($event.target as HTMLInputElement).checked)"
          />{{ prizeNames[key] }}</label
        ><input
          class="input"
          :value="prize(key)?.amount || ''"
          :disabled="!prize(key)"
          :aria-label="`${prizeNames[key]}金额`"
          type="number"
          min="0.00000001"
          max="1000000"
          step="0.00000001"
          :required="!!prize(key)"
          placeholder="—"
          @input="change(key, 'amount', ($event.target as HTMLInputElement).value)"
        /><input
          class="input"
          :value="prize(key)?.count || ''"
          :disabled="!prize(key)"
          :aria-label="`${prizeNames[key]}人数`"
          type="number"
          min="1"
          max="100000"
          step="1"
          :required="!!prize(key)"
          placeholder="—"
          @input="change(key, 'count', ($event.target as HTMLInputElement).value)"
        />
      </div>
    </div>
    <div class="lottery-budget">
      <span>{{ count }} 个中奖名额</span
      ><span
        >奖金额度 <strong>${{ lotteryMoney(total) }}</strong></span
      >
    </div>
  </section>
</template>
