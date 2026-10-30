<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import { formatTime } from '@/api/workspace'
import type { LoginRun, ReloginAccount } from '@/api/relogin'
defineProps<{ accounts: ReloginAccount[]; runs: LoginRun[]; busy: boolean }>()
const emit = defineEmits<{ saved: [account: ReloginAccount]; login: [account: ReloginAccount] }>()
</script>
<template>
  <div class="ws-table-wrap">
    <table class="ws-table">
      <thead>
        <tr>
          <th>渠道账号</th>
          <th>当前状态</th>
          <th>套餐 / 调度</th>
          <th>登录资料</th>
          <th>最近更新</th>
          <th class="text-right">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="account in accounts"
          :key="account.id"
        >
          <td>
            <strong class="font-medium">{{ account.email || account.name }}</strong>
            <p class="ws-muted mt-1">#{{ account.id }} · {{ account.name }}</p>
          </td>
          <td>
            <span
              class="ws-label"
              :class="{ 'ws-label-active': account.status === 'active' }"
              >{{
                account.status === 'active'
                  ? '正常'
                  : account.status === 'error'
                    ? '需要处理'
                    : '已暂停'
              }}</span
            >
            <p
              v-if="account.error_message"
              class="account-error"
              :title="account.error_message"
            >
              {{ account.error_message }}
            </p>
          </td>
          <td>
            <span>{{ account.plan_type || '未识别' }}</span>
            <p class="ws-muted mt-1">{{ account.schedulable ? '参与调度' : '暂停调度' }}</p>
          </td>
          <td>
            <span class="ws-muted">{{ account.has_imported ? '已保存' : '未保存' }}</span>
          </td>
          <td class="whitespace-nowrap ws-muted">{{ formatTime(account.updated_at) }}</td>
          <td>
            <div class="ws-actions justify-end flex-nowrap">
              <span
                v-if="runs.some((run) => run.email === account.email && run.status === 'running')"
                class="ws-label ws-label-active"
                ><Icon
                  name="refresh"
                  size="xs"
                  class="ws-spin mr-1"
                />重登中</span
              ><template v-else
                ><button
                  v-if="account.has_imported"
                  class="ws-button"
                  :disabled="busy"
                  @click="emit('saved', account)"
                >
                  使用资料重登</button
                ><button
                  class="ws-button ws-button-quiet"
                  :disabled="busy || !account.email"
                  @click="emit('login', account)"
                >
                  重新授权
                </button></template
              >
            </div>
          </td>
        </tr>
      </tbody>
    </table>
    <div
      v-if="!accounts.length"
      class="ws-empty"
    >
      <Icon
        name="checkCircle"
        size="lg"
      />
      <h2>没有匹配的账号</h2>
      <p>可以调整筛选条件，或通过 Refresh Token 导入账号。</p>
    </div>
  </div>
</template>
<style scoped>
.account-error {
  max-width: 220px;
  font-size: 11px;
  line-height: 1.5;
  color: #ea580c;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-top: 6px;
}
</style>
