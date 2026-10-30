<script setup lang="ts">
import { formatTime } from '@/api/workspace'
import type { ImportedPage } from '@/api/relogin'
const props = defineProps<{ result: ImportedPage | null; selected: string[] }>()
const emit = defineEmits<{ 'update:selected': [values: string[]]; page: [page: number] }>()
function toggle(email: string, checked: boolean) {
  emit(
    'update:selected',
    checked
      ? [...new Set([...props.selected, email])]
      : props.selected.filter((value) => value !== email)
  )
}
function togglePage(checked: boolean) {
  const emails = props.result?.items.map((item) => item.email) || []
  emit(
    'update:selected',
    checked
      ? [...new Set([...props.selected, ...emails])]
      : props.selected.filter((email) => !emails.includes(email))
  )
}
</script>
<template>
  <div class="ws-table-wrap">
    <table class="ws-table">
      <thead>
        <tr>
          <th>
            <input
              type="checkbox"
              aria-label="选择当前页全部资料"
              :checked="
                !!result?.items.length &&
                result.items.every((item) => selected.includes(item.email))
              "
              @change="togglePage(($event.target as HTMLInputElement).checked)"
            />
          </th>
          <th>账号邮箱</th>
          <th>验证方式</th>
          <th>已保存资料</th>
          <th>更新时间</th>
          <th>最近使用</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="item in result?.items"
          :key="item.email"
        >
          <td>
            <input
              type="checkbox"
              :aria-label="`选择 ${item.email}`"
              :checked="selected.includes(item.email)"
              @change="toggle(item.email, ($event.target as HTMLInputElement).checked)"
            />
          </td>
          <td class="font-medium">{{ item.email }}</td>
          <td>{{ item.auth_mode === 'email_otp' ? '邮箱验证码' : '密码 / MFA' }}</td>
          <td>
            <div class="ws-actions">
              <span
                v-if="item.has_password"
                class="ws-label"
                >密码</span
              ><span
                v-if="item.has_mfa"
                class="ws-label"
                >MFA 密钥</span
              ><span
                v-if="item.has_inbox"
                class="ws-label"
                >收件箱</span
              >
            </div>
          </td>
          <td class="ws-muted whitespace-nowrap">{{ formatTime(item.updated_at) }}</td>
          <td class="ws-muted whitespace-nowrap">{{ formatTime(item.last_used_at) }}</td>
        </tr>
      </tbody>
    </table>
    <div
      v-if="!result?.items.length"
      class="ws-empty"
    >
      <h2>尚未保存登录资料</h2>
      <p>导入常用账号资料后，可快速重登，并按需开启自动恢复。</p>
    </div>
    <div
      v-if="result && result.total > result.page_size"
      class="ws-pagination"
    >
      <span>共 {{ result.total }} 条资料</span>
      <div class="ws-actions">
        <button
          class="ws-button"
          :disabled="result.page <= 1"
          @click="emit('page', result.page - 1)"
        >
          上一页</button
        ><span>{{ result.page }}</span
        ><button
          class="ws-button"
          :disabled="result.page * result.page_size >= result.total"
          @click="emit('page', result.page + 1)"
        >
          下一页
        </button>
      </div>
    </div>
  </div>
</template>
