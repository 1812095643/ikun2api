<script setup lang="ts">
import { computed, reactive, shallowRef, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import FormSteps from '@/components/common/FormSteps.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import { defaultWarmupRule, type WarmupRule, type WarmupAccount } from '@/api/accountWarmup'
const props = defineProps<{ show: boolean; rule: WarmupRule | null; accounts: WarmupAccount[]; timezone: string; saving: boolean; models: { id: string; display_name: string }[]; groups: { id: number; name: string }[] }>()
const emit = defineEmits<{ close: []; save: [r: WarmupRule] }>()
const step = shallowRef(1), target = shallowRef('all'), accountSearch = shallowRef(''), planText = shallowRef('')
const form = reactive(defaultWarmupRule(props.timezone))
const weekdays = ['周日', '周一', '周二', '周三', '周四', '周五', '周六']
const groupIDs = computed(() => [...new Set(props.accounts.flatMap(a => a.group_ids || []))].sort((a, b) => a - b))
const filtered = computed(() => props.accounts.filter(a => a.name.toLowerCase().includes(accountSearch.value.toLowerCase())))
const matched = computed(() => props.accounts.filter(a => (!form.config.account_ids.length || form.config.account_ids.includes(a.id)) && (!form.config.group_ids.length || a.group_ids?.some(id => form.config.group_ids.includes(id))) && (!form.config.search || a.name.toLowerCase().includes(form.config.search.toLowerCase()))))
watch(() => props.show, () => {
 Object.assign(form, structuredClone(props.rule ? JSON.parse(JSON.stringify(props.rule)) : defaultWarmupRule(props.timezone)))
 step.value = 1; target.value = form.config.account_ids?.length ? 'accounts' : form.config.group_ids?.length ? 'groups' : 'all'
 planText.value = form.config.plans?.join(',') || ''
}, { immediate: true })
function setTarget(value: string) { target.value = value; form.config.account_ids = []; form.config.group_ids = [] }
function submit() { form.config.plans = planText.value.split(/[,，]/).map(s => s.trim()).filter(Boolean); emit('save', JSON.parse(JSON.stringify(form))) }
const valid = computed(() => !!form.name.trim() && !!form.config.model.trim() && form.config.days.length > 0 && form.config.windows.length > 0 && (target.value !== 'accounts' || form.config.account_ids.length > 0) && (target.value !== 'groups' || form.config.group_ids.length > 0))
</script>
<template>
 <BaseDialog :show="show" :title="rule?.id ? '编辑热身规则' : '新建热身规则'" width="wide" @close="emit('close')">
  <FormSteps :steps="['筛选账号', '设置时间', '请求设置']" :current="step" />
  <form @submit.prevent="step < 3 ? step++ : submit()">
   <div v-if="step === 1" class="space-y-4">
    <div class="grid gap-4 sm:grid-cols-2"><label class="text-sm">规则名称<input v-model="form.name" class="input mt-1" maxlength="80" required /></label><label class="text-sm">优先级<input v-model.number="form.priority" type="number" class="input mt-1" /><span class="text-xs text-gray-500">数字越大越优先，同账号只采用一条规则。</span></label></div>
    <div class="flex flex-wrap gap-4 text-sm"><label v-for="item in [{ value: 'all', label: '全部符合条件账号' }, { value: 'groups', label: '指定分组' }, { value: 'accounts', label: '指定账号' }]" :key="item.value" class="flex items-center gap-2"><input type="radio" name="warmup-target" :checked="target === item.value" @change="setTarget(item.value)" />{{ item.label }}</label></div>
    <div v-if="target === 'groups'" class="flex max-h-36 flex-wrap gap-3 overflow-auto rounded-lg border border-gray-200 p-3 dark:border-dark-700"><label v-for="id in groupIDs" :key="id" class="flex gap-2 text-sm"><input v-model="form.config.group_ids" type="checkbox" :value="id" />{{ groups.find(g => g.id === id)?.name || ('分组 #' + id) }}</label><span v-if="!groupIDs.length" class="text-sm text-gray-500">当前账号没有绑定分组。</span></div>
    <div v-if="target === 'accounts'" class="space-y-2"><input v-model="accountSearch" class="input" placeholder="搜索账号名称" aria-label="筛选可选账号" /><div class="max-h-44 space-y-2 overflow-auto rounded-lg border border-gray-200 p-3 dark:border-dark-700"><label v-for="a in filtered" :key="a.id" class="flex gap-2 text-sm"><input v-model="form.config.account_ids" type="checkbox" :value="a.id" />{{ a.name }}<span class="text-xs text-gray-400">#{{ a.id }} · {{ a.plan || '未知套餐' }}</span></label></div></div>
    <div class="grid gap-4 sm:grid-cols-2"><label class="text-sm">名称包含<input v-model="form.config.search" class="input mt-1" placeholder="可留空" /></label><label class="text-sm">套餐筛选<input v-model="planText" class="input mt-1" placeholder="如 pro,max；留空不限" /></label></div>
    <p class="text-sm text-gray-500">仅针对 Claude OAuth 主账号。429、停用、额度查询失败，或五小时/周用量超过 99% 的账号会跳过。</p>
   </div>
   <div v-else-if="step === 2" class="space-y-4">
    <div class="grid gap-4 sm:grid-cols-2"><label class="text-sm">执行时区<input v-model="form.config.timezone" class="input mt-1" list="warmup-timezones" required /><datalist id="warmup-timezones"><option>Asia/Shanghai</option><option>Asia/Singapore</option><option>UTC</option></datalist></label><label class="text-sm">仅执行于（可留空）<input v-model="form.config.date" type="date" class="input mt-1" /></label></div>
    <div class="flex flex-wrap gap-3"><label v-for="(day, index) in weekdays" :key="index" class="flex items-center gap-2 text-sm"><input v-model="form.config.days" type="checkbox" :value="index" />{{ day }}</label></div>
    <div v-for="(w, index) in form.config.windows" :key="index" class="flex flex-wrap items-end gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-700"><label class="text-sm">开始<input v-model="w.start" type="time" class="input mt-1" required /></label><label class="text-sm">结束<input v-model="w.end" type="time" class="input mt-1" required /></label><label class="w-24 text-sm">轮数<input v-model.number="w.rounds" type="number" min="1" max="6" class="input mt-1" /></label><button class="btn btn-ghost btn-sm" type="button" :disabled="form.config.windows.length === 1" @click="form.config.windows.splice(index, 1)">移除</button></div>
    <button class="btn btn-secondary" type="button" :disabled="form.config.windows.length >= 8" @click="form.config.windows.push({ start: '06:30', end: '07:30', rounds: 1 })">添加时段</button>
    <p class="text-sm text-gray-500">结束早于开始表示跨午夜。多轮均匀分布在时段内；错过的轮次不集中补跑。</p>
   </div>
   <div v-else class="space-y-4">
    <label class="block text-sm">模型<Select v-model="form.config.model" :options="models.map(m => ({ value: m.id, label: m.display_name + ' · ' + m.id }))" searchable class="mt-1" placeholder="选择模型" /><span class="mt-1 block text-xs text-gray-500">使用账号允许的模型及映射，发布更新不会自动替换。</span></label>
    <div class="grid gap-4 sm:grid-cols-3"><label class="text-sm">每轮请求次数<input v-model.number="form.config.count" type="number" min="1" max="5" class="input mt-1" /></label><label class="text-sm">消息间隔（秒）<input v-model.number="form.config.message_gap" type="number" min="5" max="300" class="input mt-1" /></label><label class="text-sm">账号间隔（秒）<input v-model.number="form.config.account_gap" type="number" min="5" max="3600" class="input mt-1" /></label><label class="text-sm">单次超时（秒）<input v-model.number="form.config.timeout" type="number" min="5" max="120" class="input mt-1" /></label><label class="text-sm">每账号每日请求上限<input v-model.number="form.config.daily_max" type="number" min="1" max="20" class="input mt-1" /></label><label class="text-sm">最近使用后避让（分钟）<input v-model.number="form.config.idle_minutes" type="number" min="0" max="1440" class="input mt-1" /></label></div>
    <label class="flex items-center justify-between gap-4 text-sm"><span>已有活跃五小时窗口时跳过</span><Toggle v-model="form.config.skip_active" /></label>
    <label v-if="form.id" class="flex items-center justify-between gap-4 text-sm"><span>启用此规则</span><Toggle v-model="form.enabled" /></label>
    <div class="rounded-lg bg-gray-50 p-4 text-sm text-gray-600 dark:bg-dark-800 dark:text-gray-300">匹配约 {{ matched.length }} 个账号，每轮最多 {{ matched.length * form.config.count }} 条短请求。全站热身并发为 1；执行前串行主动刷新额度，实际用量单独记录。</div>
   </div>
   <div class="mt-6 flex justify-end gap-2"><button type="button" class="btn btn-secondary" @click="step > 1 ? step-- : emit('close')">{{ step > 1 ? '上一步' : '取消' }}</button><button v-if="step < 3" type="submit" class="btn btn-primary" :disabled="step === 1 && !form.name.trim()">下一步</button><button v-else type="submit" class="btn btn-primary" :disabled="saving || !valid">{{ saving ? '保存中…' : form.id ? '保存' : '保存为暂停规则' }}</button></div>
  </form>
 </BaseDialog>
</template>
