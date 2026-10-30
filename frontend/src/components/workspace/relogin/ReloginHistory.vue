<script setup lang="ts">
import { formatTime } from '@/api/workspace'
import { runLabels, type LoginRun } from '@/api/relogin'
defineProps<{ runs: LoginRun[]; busy: boolean }>()
const emit = defineEmits<{ verify: [run: LoginRun]; apply: [run: LoginRun]; export: [run: LoginRun] }>()
</script>
<template>
  <div class="ws-table-wrap">
    <table class="ws-table">
      <thead>
        <tr>
          <th>账号</th>
          <th>来源</th>
          <th>结果</th>
          <th>开始时间</th>
          <th>说明</th>
          <th class="text-right">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="run in runs"
          :key="run.id"
        >
          <td>
            <strong class="font-medium">{{ run.email }}</strong>
            <p class="ws-muted mt-1">任务 #{{ run.id }}</p>
          </td>
          <td>
            {{
              run.trigger === 'auto'
                ? '自动重登'
                : run.trigger === 'refresh_token'
                  ? 'RT 导入'
                  : '手动授权'
            }}
          </td>
          <td>
            <span
              class="ws-label"
              :class="{ 'ws-label-active': ['applied', 'success', 'running'].includes(run.status) }"
              >{{ runLabels[run.status] || run.status }}</span
            >
          </td>
          <td class="whitespace-nowrap ws-muted">{{ formatTime(run.started_at) }}</td>
          <td class="max-w-xs text-xs leading-6">
            {{ run.error_message || (run.plan_type ? `套餐：${run.plan_type}` : '—') }}
          </td>
          <td class="text-right">
            <button
              v-if="run.status === 'challenge'"
              class="ws-button"
              :disabled="busy"
              @click="emit('verify', run)"
            >
              完成验证</button
            ><button
              v-else-if="['success', 'apply_failed'].includes(run.status) && !run.applied"
              class="ws-button"
              :disabled="busy"
              @click="emit('apply', run)"
            >
              写回账号</button
            >
            <button
              v-if="['success', 'applied', 'paused_free', 'apply_failed'].includes(run.status)"
              class="ws-button ws-button-quiet"
              :disabled="busy"
              @click="emit('export', run)"
            >导出凭据</button>
          </td>
        </tr>
      </tbody>
    </table>
    <div
      v-if="!runs.length"
      class="ws-empty"
    >
      <h2>尚无授权记录</h2>
      <p>每次重登的状态和结果都会保存在这里。</p>
    </div>
  </div>
</template>
