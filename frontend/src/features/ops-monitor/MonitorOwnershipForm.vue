<script setup lang="ts">
import type { MonitorLegacyHandoff, MonitorOwnerOverride, MonitorRule } from '@/api/admin/monitor'
defineProps<{ rules: MonitorRule[] }>()
const overrides = defineModel<MonitorOwnerOverride[]>('overrides', { required: true })
const handoffs = defineModel<MonitorLegacyHandoff[]>('handoffs', { required: true })

function addOverride() {
  overrides.value = [...(overrides.value ?? []), { rule_id: 'U01', scope: '', model: '', notification_owner: 'legacy' }]
}
function addHandoff() {
  handoffs.value = [...(handoffs.value ?? []), { legacy_rule_id: 0, platform: '', monitor_rules: ['U01'] }]
}
function groupValue(event: Event): number | undefined { const value = (event.target as HTMLInputElement).value; return value === '' ? undefined : Number(value) }
</script>
<template>
  <section class="space-y-4 border-t border-gray-200 pt-5 dark:border-dark-700">
    <h3 class="font-semibold">按范围接管与旧通知交接</h3>
    <p class="text-sm text-gray-500">范围规则可覆盖上方默认负责人。精确事件范围优先，其次为分组、模型；填写多个条件时需要同时匹配。</p>
    <div v-for="(item, index) in overrides" :key="index" class="space-y-3 rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <label class="text-xs">规则<select v-model="item.rule_id" class="input mt-1"><option v-for="rule in rules" :key="rule.id" :value="rule.id">{{ rule.id }} {{ rule.title }}</option></select></label>
        <label class="text-xs">分组 ID（留空表示任意）<input :value="item.group_id" type="number" min="0" class="input mt-1" @input="item.group_id = groupValue($event)"></label>
        <label class="text-xs">模型（留空表示任意）<input v-model="item.model" class="input mt-1"></label>
        <label class="text-xs">精确事件范围（从详情复制）<input v-model="item.scope" class="input mt-1" placeholder="至少指定一项范围"></label>
        <label class="text-xs">通知负责人<select v-model="item.notification_owner" class="input mt-1"><option value="legacy">保留旧通知</option><option value="monitor">由新模块通知</option></select></label>
      </div>
      <button type="button" class="text-sm text-red-600" @click="overrides = overrides.filter((_, i) => i !== index)">删除范围</button>
    </div>
    <button type="button" class="btn btn-secondary" @click="addOverride">添加接管范围</button>
    <p class="text-sm text-amber-700 dark:text-amber-300">旧规则映射必须按旧后台实际规则 ID 填写。仅在正式模式、对应新规则全部启用并完整接管该范围时，才停止该旧规则的邮件发送；旧告警记录仍保留。若有仍由旧通知负责的例外，此映射不会生效，应缩小交接范围。</p>
    <div v-for="(item, index) in handoffs" :key="index" class="space-y-3 rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <label class="text-xs">旧告警规则 ID<input v-model.number="item.legacy_rule_id" type="number" min="1" class="input mt-1"></label>
        <label class="text-xs">分组 ID（留空表示任意）<input :value="item.group_id" type="number" min="0" class="input mt-1" @input="item.group_id = groupValue($event)"></label>
        <label class="text-xs">平台（留空表示任意）<input v-model="item.platform" class="input mt-1" placeholder="例如 openai"></label>
      </div>
      <label class="block text-xs">由以下新规则共同接替（可多选）<select v-model="item.monitor_rules" multiple class="input mt-1 h-28"><option v-for="rule in rules.filter(r => r.id !== 'Q04')" :key="rule.id" :value="rule.id">{{ rule.id }} {{ rule.title }}</option></select></label>
      <button type="button" class="text-sm text-red-600" @click="handoffs = handoffs.filter((_, i) => i !== index)">删除旧规则映射</button>
    </div>
    <button type="button" class="btn btn-secondary" @click="addHandoff">添加旧规则映射</button>
    <p class="text-xs text-gray-500">恢复影子模式会停止新通知并恢复旧通知出口。切换前应保留配置版本；已经进入外部发送过程的消息无法撤回。</p>
  </section>
</template>
