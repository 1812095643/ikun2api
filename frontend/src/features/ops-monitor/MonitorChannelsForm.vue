<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { monitorAPI, monitorError, monitorTime, type MonitorChannels, type MonitorChannelsInput } from '@/api/admin/monitor'
const emit = defineEmits<{ saved: [] }>()
const view = ref<MonitorChannels | null>(null); const loadError = ref('')
const feishu = ref<MonitorChannelsInput['feishu']>({ domain: 'feishu', app_id: '', app_secret: '' })
const email = ref<MonitorChannelsInput['email']>({ host: '', port: 465, security: 'ssl', username: '', password: '', from: '' })
const busy = ref<'' | 'feishu' | 'email'>(''); const message = ref<{ part: string; ok: boolean; text: string } | null>(null)
const presets = [
  { name: 'QQ 邮箱', host: 'smtp.qq.com', port: 465, security: 'ssl' },
  { name: '腾讯企业邮', host: 'smtp.exmail.qq.com', port: 465, security: 'ssl' },
  { name: '163 邮箱', host: 'smtp.163.com', port: 465, security: 'ssl' },
  { name: '阿里企业邮', host: 'smtp.qiye.aliyun.com', port: 465, security: 'ssl' },
  { name: 'Gmail', host: 'smtp.gmail.com', port: 465, security: 'ssl' },
  { name: 'Outlook', host: 'smtp.office365.com', port: 587, security: 'starttls' },
] as const
const sourceText: Record<string, string> = { page: '本页面配置', env: '服务器 monitor.env（在本页保存后改用页面配置）', none: '未配置' }
function fill(v: MonitorChannels) {
  view.value = v
  feishu.value = { domain: v.feishu.domain || 'feishu', app_id: v.feishu.app_id || '', app_secret: '' }
  email.value = { host: v.email.host || '', port: v.email.port || 465, security: v.email.security || 'ssl', username: v.email.username || '', password: '', from: v.email.from || '' }
}
async function load() { loadError.value = ''; try { fill(await monitorAPI.channels()) } catch (e) { loadError.value = monitorError(e) } }
function usePreset(p: typeof presets[number]) { email.value.host = p.host; email.value.port = p.port; email.value.security = p.security }
// Each part saves the whole settings object; the other part is sent unchanged (blank secret keeps the stored one).
async function save(part: 'feishu' | 'email', clear = false) {
  if (!view.value) return
  busy.value = part; message.value = null
  const input: MonitorChannelsInput = { feishu: { ...feishu.value }, email: { ...email.value } }
  if (part === 'feishu') input.email = { ...view.value.email, password: '' }
  else input.feishu = { ...view.value.feishu, app_secret: '' }
  if (clear && part === 'feishu') input.feishu = { domain: 'feishu', app_id: '', app_secret: '' }
  if (clear && part === 'email') input.email = { host: '', port: 0, security: 'ssl', username: '', password: '', from: '' }
  try {
    fill(await monitorAPI.saveChannels(input))
    message.value = { part, ok: true, text: clear ? '已清除' : part === 'feishu' ? '已保存，飞书凭据验证通过' : '已保存；建议在下方收件人处发送测试邮件确认' }
    emit('saved')
  } catch (e) { message.value = { part, ok: false, text: monitorError(e) } } finally { busy.value = '' }
}
onMounted(load)
</script>
<template>
  <section aria-label="通知通道">
    <p v-if="loadError" role="alert" class="mb-4 rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">{{ loadError }}</p>
    <div v-if="view" class="divide-y divide-gray-200 dark:divide-dark-700">
      <form class="settings-row" aria-label="飞书机器人" @submit.prevent="save('feishu')">
        <div>
          <div class="flex items-center gap-2"><h3 class="settings-title">飞书机器人</h3><span class="rounded-full px-2 py-0.5 text-xs" :class="view.feishu_secret_set ? 'bg-primary-50 text-primary-700 dark:bg-primary-950 dark:text-primary-300' : 'bg-gray-100 text-gray-500 dark:bg-dark-800'">{{ view.feishu_secret_set ? '已接入' : '未接入' }}</span></div>
          <p class="settings-desc">用飞书企业自建应用的机器人发通知。保存时会先向飞书验证一次。</p>
          <details class="mt-2 text-xs text-gray-500"><summary class="cursor-pointer text-primary-700 dark:text-primary-400">怎么创建机器人</summary><ol class="mt-2 list-decimal space-y-1 pl-4 leading-5"><li>飞书开放平台创建「企业自建应用」，添加「机器人」能力。</li><li>开通权限：以应用身份发消息（im:message:send_as_bot）、获取群组信息（im:chat:readonly）。</li><li>发布版本，把机器人拉进要接收通知的群。</li><li>复制 App ID 和 App Secret 填到右边。不是群 Webhook，也不需要配置事件订阅。</li></ol></details>
        </div>
        <div class="space-y-3">
          <div class="inline-flex rounded-lg bg-gray-100 p-0.5 text-sm dark:bg-dark-800" role="radiogroup" aria-label="飞书版本">
            <button v-for="d in [{ id: 'feishu', t: '飞书（国内）' }, { id: 'lark', t: 'Lark（海外）' }]" :key="d.id" type="button" role="radio" :aria-checked="feishu.domain === d.id" class="rounded-md px-3 py-1" :class="feishu.domain === d.id ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white' : 'text-gray-500'" @click="feishu.domain = d.id as 'feishu' | 'lark'">{{ d.t }}</button>
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
            <label class="text-sm text-gray-700 dark:text-gray-300">App ID<input v-model="feishu.app_id" class="input mt-1 w-full font-mono text-sm" placeholder="cli_…" autocomplete="off"></label>
            <label class="text-sm text-gray-700 dark:text-gray-300">App Secret<input v-model="feishu.app_secret" type="password" class="input mt-1 w-full" :placeholder="view.feishu_secret_set ? '已保存，留空保持不变' : '在「凭证与基础信息」中复制'" autocomplete="new-password"></label>
          </div>
          <div class="flex flex-wrap items-center gap-3">
            <button class="btn btn-primary" type="submit" :disabled="busy !== ''">{{ busy === 'feishu' ? '正在验证…' : '验证并保存' }}</button>
            <button v-if="view.feishu_secret_set && view.source === 'page'" type="button" class="text-sm text-gray-500 hover:text-red-600" :disabled="busy !== ''" @click="save('feishu', true)">清除</button>
            <p v-if="message?.part === 'feishu'" :role="message.ok ? 'status' : 'alert'" class="text-sm" :class="message.ok ? 'text-primary-700 dark:text-primary-400' : 'text-red-600'">{{ message.text }}</p>
          </div>
        </div>
      </form>

      <form class="settings-row" aria-label="邮箱 SMTP" @submit.prevent="save('email')">
        <div>
          <div class="flex items-center gap-2"><h3 class="settings-title">邮箱</h3><span class="rounded-full px-2 py-0.5 text-xs" :class="view.email.host ? 'bg-primary-50 text-primary-700 dark:bg-primary-950 dark:text-primary-300' : 'bg-gray-100 text-gray-500 dark:bg-dark-800'">{{ view.email.host ? '已接入' : '未接入' }}</span></div>
          <p class="settings-desc">只发严重问题：新故障、升级和恢复各一封，普通更新不发邮件。QQ、163 等邮箱要用授权码，不是登录密码。</p>
        </div>
        <div class="space-y-3">
          <div class="flex flex-wrap items-center gap-1.5"><span class="mr-1 text-xs text-gray-500">快速填写</span><button v-for="p in presets" :key="p.name" type="button" class="rounded-full border border-gray-200 px-2.5 py-0.5 text-xs text-gray-700 hover:border-primary-400 hover:text-primary-700 dark:border-dark-600 dark:text-gray-300" @click="usePreset(p)">{{ p.name }}</button></div>
          <div class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_6rem_10rem]">
            <label class="text-sm text-gray-700 dark:text-gray-300">SMTP 服务器<input v-model="email.host" class="input mt-1 w-full" placeholder="smtp.qq.com"></label>
            <label class="text-sm text-gray-700 dark:text-gray-300">端口<input v-model.number="email.port" type="number" min="1" max="65535" class="input mt-1 w-full tabular-nums"></label>
            <label class="text-sm text-gray-700 dark:text-gray-300">加密<select v-model="email.security" class="input mt-1 w-full"><option value="ssl">SSL（465）</option><option value="starttls">STARTTLS（587）</option></select></label>
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
            <label class="text-sm text-gray-700 dark:text-gray-300">登录账号<input v-model="email.username" class="input mt-1 w-full" placeholder="通常就是邮箱地址" autocomplete="off"></label>
            <label class="text-sm text-gray-700 dark:text-gray-300">授权码<input v-model="email.password" type="password" class="input mt-1 w-full" :placeholder="view.email_password_set ? '已保存，留空保持不变' : '邮箱设置里生成的授权码'" autocomplete="new-password"></label>
          </div>
          <label class="block text-sm text-gray-700 dark:text-gray-300">发件人<input v-model="email.from" class="input mt-1 w-full" placeholder="心链监控 <ops@example.com>"></label>
          <div class="flex flex-wrap items-center gap-3">
            <button class="btn btn-primary" type="submit" :disabled="busy !== ''">{{ busy === 'email' ? '正在保存…' : '保存' }}</button>
            <button v-if="view.email.host && view.source === 'page'" type="button" class="text-sm text-gray-500 hover:text-red-600" :disabled="busy !== ''" @click="save('email', true)">清除</button>
            <p v-if="message?.part === 'email'" :role="message.ok ? 'status' : 'alert'" class="text-sm" :class="message.ok ? 'text-primary-700 dark:text-primary-400' : 'text-red-600'">{{ message.text }}</p>
          </div>
        </div>
      </form>
      <p class="pt-3 text-xs text-gray-500">密钥加密保存，保存后不再显示。当前生效：{{ sourceText[view.source] }}<template v-if="view.updated_by">，{{ monitorTime(view.updated_at) }} 由 {{ view.updated_by }} 修改</template>。</p>
    </div>
  </section>
</template>

<style scoped>
.settings-row { @apply grid gap-x-10 gap-y-4 py-7 md:grid-cols-[15rem_minmax(0,1fr)]; }
.settings-title { @apply text-sm font-semibold text-gray-900 dark:text-white; }
.settings-desc { @apply mt-1 text-sm leading-6 text-gray-500 dark:text-gray-400; }
</style>
