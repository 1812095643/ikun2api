<script setup lang="ts">
import { ref } from 'vue'
import MonitorOwnershipForm from './MonitorOwnershipForm.vue'
import MonitorSwitch from './MonitorSwitch.vue'
import { computed } from 'vue'
import { monitorAPI, monitorError, type MonitorConfig, type MonitorRule, type MonitorProfile } from '@/api/admin/monitor'
const props = defineProps<{ config: MonitorConfig; rules: MonitorRule[]; transports: Record<string, boolean> }>()
const emit = defineEmits<{ saved: [] }>()
const draft = ref<MonitorConfig>(JSON.parse(JSON.stringify(props.config)))
draft.value.owner_overrides ??= []; draft.value.legacy_handoffs ??= []
for (const rule of props.rules) draft.value.rules[rule.id] ??= { enabled: true, notification_owner: 'legacy', version: 1 }
const feishu = ref(draft.value.feishu_targets?.join('\n') || '')
const emails = ref(draft.value.email_targets?.join('\n') || '')
const chats = ref<{ chat_id: string; name: string }[] | null>(null); const botName = ref(''); const chatBusy = ref(false); const chatError = ref('')
const testing = ref(false); const testResults = ref<{ target: string; ok: boolean; text: string }[]>([])
const lines = (v: string) => v.split(/\s+/).filter(Boolean)
const picked = (id: string) => lines(feishu.value).includes(id)
function toggleChat(id: string) { const list = lines(feishu.value); feishu.value = (picked(id) ? list.filter(x => x !== id) : [...list, id]).join('\n') }
async function loadChats() { chatBusy.value = true; chatError.value = ''; try { const r = await monitorAPI.feishuChats(); chats.value = r.chats; botName.value = r.bot_name } catch (e) { chatError.value = monitorError(e) } finally { chatBusy.value = false } }
async function sendTests() {
  testing.value = true; testResults.value = []
  const jobs: [('feishu' | 'email'), string][] = [...lines(feishu.value).map(t => ['feishu', t] as ['feishu', string]), ...lines(emails.value).map(t => ['email', t] as ['email', string])]
  for (const [channel, target] of jobs) {
    try { await monitorAPI.testChannel(channel, target); testResults.value.push({ target, ok: true, text: '已发送' }) } catch (e) { testResults.value.push({ target, ok: false, text: monitorError(e) }) }
  }
  testing.value = false
}
const balance = ref(draft.value.balance_low_usd?.toString() || '')
const ratio = ref(draft.value.subscription_low_ratio == null ? '' : (draft.value.subscription_low_ratio * 100).toString())
const model = ref(''); const busy = ref(false); const error = ref(''); const notice = ref('')
const timings: { key: keyof MonitorProfile; title: string }[] = [{ key: 'queue_warn_seconds', title: '排队提醒' }, { key: 'queue_fail_seconds', title: '排队严重' }, { key: 'first_warn_seconds', title: '首字提醒' }, { key: 'first_fail_seconds', title: '首字严重' }, { key: 'stall_warn_seconds', title: '输出停顿' }, { key: 'task_sla_seconds', title: '异步任务时限' }]
const live = computed({ get: () => !draft.value.shadow, set: (v: boolean) => { draft.value.shadow = !v } })
const ruleGroups = computed(() => [
  { title: '上游与请求', ids: ['U01', 'U02', 'U03', 'F01', 'F02', 'R01', 'R02', 'C01'] },
  { title: '账号池', ids: ['S01', 'S02', 'S03'] },
  { title: '速度', ids: ['L01', 'L02', 'L03', 'L04'] },
  { title: '额度与计费', ids: ['Q01', 'Q02', 'Q03', 'Q04', 'Q05', 'A01', 'B01', 'B02', 'B03'] },
  { title: '服务器与监控自身', ids: ['H01', 'H02', 'M01', 'M02'] },
].map(g => ({ ...g, rules: g.ids.map(id => props.rules.find(r => r.id === id)).filter((r): r is MonitorRule => Boolean(r)) })).concat([{ title: '其他', ids: [], rules: props.rules.filter(r => !/^(U0[1-3]|F0[12]|R0[12]|C01|S0[1-3]|L0[1-4]|Q0[1-5]|A01|B0[1-3]|H0[12]|M0[12])$/.test(r.id)) }]).filter(g => g.rules.length))
const enabledCount = computed(() => props.rules.filter(r => draft.value.rules[r.id]?.enabled).length)
function addProfile() { const name = model.value.trim(); if (!name || draft.value.profiles[name]) return; draft.value.profiles[name] = { ...draft.value.profiles.default }; model.value = '' }
async function save() {
  error.value = ''; notice.value = ''; busy.value = true
  const next: MonitorConfig = JSON.parse(JSON.stringify(draft.value))
  next.feishu_targets = lines(feishu.value); next.email_targets = lines(emails.value)
  next.balance_low_usd = balance.value.trim() === '' ? null : Number(balance.value)
  next.subscription_low_ratio = ratio.value.trim() === '' ? null : Number(ratio.value) / 100
  if ([next.balance_low_usd, next.subscription_low_ratio].some(value => value != null && !Number.isFinite(value))) { error.value = '额度阈值必须是有效数字'; busy.value = false; return }
  for (const profile of Object.values(next.profiles)) profile.stall_fail_seconds = Math.max(profile.stall_fail_seconds, profile.stall_warn_seconds)
  next.idempotency_key = crypto.randomUUID()
  try { draft.value = await monitorAPI.save(next); notice.value = `已保存配置 v${draft.value.version}`; emit('saved') } catch (e) { error.value = monitorError(e) } finally { busy.value = false }
}
</script>
<template>
  <form class="pb-20" @submit.prevent="save">
    <div class="divide-y divide-gray-200 dark:divide-dark-700">
      <section class="settings-row">
        <div><h3 class="settings-title">发送通知</h3><p class="settings-desc">关闭时只在页面记录问题，不发飞书或邮件，适合刚接入时先观察。</p></div>
        <div class="flex items-center gap-3">
          <MonitorSwitch v-model="live" label="发送通知" />
          <span class="text-sm" :class="live ? 'text-gray-900 dark:text-white' : 'text-amber-700 dark:text-amber-300'">{{ live ? '正在发送通知' : '影子模式：只记录，不发送' }}</span>
        </div>
      </section>

      <section class="settings-row">
        <div><h3 class="settings-title">收件人</h3><p class="settings-desc">飞书收到所有通知；邮箱只收严重问题（新故障、升级、恢复）。改完先发测试，确认收到再保存。</p></div>
        <div class="space-y-4">
          <div>
            <div class="mb-1.5 flex items-center justify-between gap-2"><label for="m-feishu" class="text-sm font-medium text-gray-800 dark:text-gray-200">飞书群或个人</label><button v-if="transports.feishu" type="button" class="text-sm text-primary-700 hover:underline dark:text-primary-400" :disabled="chatBusy" @click="loadChats">{{ chatBusy ? '正在读取…' : '从飞书选择群' }}</button></div>
            <textarea id="m-feishu" v-model="feishu" rows="2" class="input w-full font-mono text-sm" placeholder="每行一个：群 chat_id（oc_…）或个人 open_id（ou_…）" />
            <p v-if="!transports.feishu" class="mt-1 text-xs text-amber-700 dark:text-amber-300">飞书机器人还没接入，先在上方「通知通道」填写。</p>
            <p v-if="chatError" role="alert" class="mt-1 text-xs text-red-600">{{ chatError }}</p>
            <div v-if="chats" class="mt-2 rounded-lg border border-gray-200 p-3 dark:border-dark-700">
              <p class="mb-2 text-xs text-gray-500">机器人「{{ botName }}」所在的群。没看到的群，先把机器人拉进群再点一次。</p>
              <p v-if="!chats.length" class="text-sm text-gray-500">机器人还没有加入任何群。</p>
              <label v-for="c in chats" :key="c.chat_id" class="flex items-center gap-2 py-1 text-sm"><input type="checkbox" class="rounded" :checked="picked(c.chat_id)" @change="toggleChat(c.chat_id)">{{ c.name || '未命名群' }}<span class="truncate font-mono text-xs text-gray-400">{{ c.chat_id }}</span></label>
            </div>
          </div>
          <div>
            <label for="m-email" class="mb-1.5 block text-sm font-medium text-gray-800 dark:text-gray-200">邮箱</label>
            <textarea id="m-email" v-model="emails" rows="2" class="input w-full text-sm" placeholder="每行一个邮箱地址" />
            <p v-if="!transports.email" class="mt-1 text-xs text-amber-700 dark:text-amber-300">邮箱还没接入，先在上方「通知通道」填写 SMTP。</p>
          </div>
          <div class="flex flex-wrap items-center gap-3">
            <button type="button" class="btn btn-secondary" :disabled="testing || (!lines(feishu).length && !lines(emails).length)" @click="sendTests">{{ testing ? '正在发送…' : '给以上收件人发送测试通知' }}</button>
            <span class="text-xs text-gray-500">不用先保存。</span>
          </div>
          <ul v-if="testResults.length" class="space-y-1 text-sm"><li v-for="r in testResults" :key="r.target" class="flex gap-2" :class="r.ok ? 'text-primary-700 dark:text-primary-400' : 'text-red-600'"><span class="font-mono text-xs leading-5">{{ r.target }}</span><span>{{ r.text }}</span></li></ul>
        </div>
      </section>

      <section class="settings-row">
        <div><h3 class="settings-title">卡片里的详情链接</h3><p class="settings-desc">通知卡片底部「查看详情」打开的站点地址，需要 HTTPS。</p></div>
        <div><input v-model="draft.detail_base_url" type="url" class="input w-full" placeholder="https://你的站点域名" aria-label="后台站点地址"><p class="mt-1.5 text-xs text-gray-500">飞书使用应用机器人发送，不是群 Webhook 链接。未接入任何通道时请保持影子模式。</p></div>
      </section>

      <section class="settings-row">
        <div><h3 class="settings-title">额度提醒</h3><p class="settings-desc">用户钱包或套餐快用完时提醒。留空表示不提醒；余额回到阈值的 110% 才算恢复。额度彻底用完总是会提醒。</p></div>
        <div class="grid gap-4 sm:grid-cols-2">
          <label class="text-sm text-gray-700 dark:text-gray-300">钱包余额低于<div class="mt-1 flex items-center rounded-lg border border-gray-300 focus-within:border-primary-500 dark:border-dark-600"><span class="pl-3 text-gray-400">$</span><input v-model="balance" type="number" min="0" step="any" class="w-full bg-transparent px-2 py-2 outline-none" placeholder="不提醒"></div></label>
          <label class="text-sm text-gray-700 dark:text-gray-300">套餐剩余低于<div class="mt-1 flex items-center rounded-lg border border-gray-300 focus-within:border-primary-500 dark:border-dark-600"><input v-model="ratio" type="number" min="0" max="99.99" step="any" class="w-full bg-transparent px-3 py-2 outline-none" placeholder="不提醒"><span class="pr-3 text-gray-400">%</span></div></label>
          <label class="text-sm text-gray-700 dark:text-gray-300">支付后多久未到账算异常<div class="mt-1 flex items-center rounded-lg border border-gray-300 focus-within:border-primary-500 dark:border-dark-600"><input v-model.number="draft.payment_grace_seconds" type="number" min="30" max="86400" class="w-full bg-transparent px-3 py-2 outline-none"><span class="pr-3 text-gray-400">秒</span></div></label>
          <label class="text-sm text-gray-700 dark:text-gray-300">任务完成后多久未扣费算异常<div class="mt-1 flex items-center rounded-lg border border-gray-300 focus-within:border-primary-500 dark:border-dark-600"><input v-model.number="draft.billing_grace_seconds" type="number" min="30" max="86400" class="w-full bg-transparent px-3 py-2 outline-none"><span class="pr-3 text-gray-400">秒</span></div></label>
        </div>
      </section>

      <section class="settings-row">
        <div><h3 class="settings-title">等待多久算慢</h3><p class="settings-desc">单位为秒。单个慢请求只记录；同一渠道 10 分钟内有 5 个以上慢请求才通知。可以为特定模型单独设置。</p></div>
        <div>
          <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
            <table class="w-full min-w-[560px] text-sm">
              <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-800"><tr><th class="px-3 py-2 text-left font-medium">模型</th><th v-for="f in timings" :key="f.key" class="px-2 py-2 text-right font-medium">{{ f.title }}</th><th class="w-8" /></tr></thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                <tr v-for="(profile, name) in draft.profiles" :key="name">
                  <td class="max-w-[10rem] truncate px-3 py-1.5 text-gray-800 dark:text-gray-200" :title="String(name)">{{ name === 'default' ? '所有模型（默认）' : name }}</td>
                  <td v-for="f in timings" :key="f.key" class="px-1 py-1"><input v-model.number="profile[f.key]" type="number" min="0" class="w-full rounded-md border border-transparent bg-transparent px-2 py-1 text-right tabular-nums hover:border-gray-200 focus:border-primary-500 focus:outline-none dark:hover:border-dark-600" :aria-label="`${name} ${f.title}`"></td>
                  <td class="pr-2 text-right"><button v-if="name !== 'default'" type="button" class="rounded p-1 text-gray-400 hover:text-red-600" :aria-label="`删除 ${name}`" @click="delete draft.profiles[name]">×</button></td>
                </tr>
              </tbody>
            </table>
          </div>
          <div class="mt-2 flex gap-2"><input v-model="model" class="input min-w-0 flex-1 py-1.5 text-sm" aria-label="新增模型档位" placeholder="输入准确的模型名，例如 claude-opus-5-5" @keydown.enter.prevent="addProfile"><button type="button" class="btn btn-secondary py-1.5" @click="addProfile">单独设置</button></div>
        </div>
      </section>

      <section class="settings-row">
        <div><h3 class="settings-title">检查哪些问题</h3><p class="settings-desc">关掉的规则不再检查。「交给旧告警」表示这条规则由站点原来的告警邮件负责，这里只记录不通知，避免重复提醒。</p><p class="mt-2 text-xs text-gray-500">已开启 {{ enabledCount }} / {{ rules.length }} 条</p></div>
        <div class="space-y-5">
          <div v-for="g in ruleGroups" :key="g.title">
            <h4 class="mb-1 text-xs font-medium text-gray-500">{{ g.title }}</h4>
            <ul class="divide-y divide-gray-100 rounded-lg border border-gray-200 dark:divide-dark-800 dark:border-dark-700">
              <li v-for="rule in g.rules" :key="rule.id" class="flex items-center gap-3 px-3 py-2">
                <MonitorSwitch v-model="draft.rules[rule.id].enabled" :label="`检查：${rule.title}`" :disabled="rule.id === 'Q04'" />
                <span class="min-w-0 flex-1"><span class="block truncate text-sm text-gray-800 dark:text-gray-200" :class="draft.rules[rule.id].enabled ? '' : 'text-gray-400 dark:text-gray-500'" :title="rule.advice">{{ rule.title }}</span></span>
                <select v-model="draft.rules[rule.id].notification_owner" :aria-label="`${rule.id} 通知负责人`" :disabled="rule.id === 'Q04' || !draft.rules[rule.id].enabled" class="rounded-md border border-gray-200 bg-transparent py-1 pl-2 pr-7 text-xs text-gray-600 disabled:opacity-40 dark:border-dark-600 dark:text-gray-300"><option value="monitor">由这里通知</option><option value="legacy">交给旧告警</option></select>
              </li>
            </ul>
          </div>
        </div>
      </section>

      <section class="py-6">
        <details class="group">
          <summary class="cursor-pointer select-none text-sm font-medium text-gray-700 dark:text-gray-300">高级：按分组或模型单独指定通知方，以及旧告警规则交接</summary>
          <MonitorOwnershipForm
            v-model:overrides="draft.owner_overrides"
            v-model:handoffs="draft.legacy_handoffs"
            class="mt-4"
            :rules="rules"
          />
        </details>
      </section>
    </div>

    <div class="sticky bottom-0 z-10 -mx-4 flex flex-wrap items-center gap-3 border-t border-gray-200 bg-white/90 px-4 py-3 backdrop-blur sm:-mx-6 sm:px-6 dark:border-dark-700 dark:bg-dark-950/90">
      <button class="btn btn-primary" type="submit" :disabled="busy">{{ busy ? '正在保存…' : '保存设置' }}</button>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      <p v-else-if="notice" role="status" class="text-sm text-primary-700 dark:text-primary-400">{{ notice }}</p>
      <span v-else class="text-xs text-gray-500">当前版本 v{{ draft.version }}。改阈值后，之前的记录保留，新问题按新阈值判断。</span>
    </div>
  </form>
</template>

<style scoped>
.settings-row { @apply grid gap-x-10 gap-y-4 py-7 md:grid-cols-[15rem_minmax(0,1fr)]; }
.settings-title { @apply text-sm font-semibold text-gray-900 dark:text-white; }
.settings-desc { @apply mt-1 text-sm leading-6 text-gray-500 dark:text-gray-400; }
</style>
