<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PoolAccount, PoolWindow } from '@/api/pool'
import DataTable from '@/components/common/DataTable.vue'
import type { Column } from '@/components/common/types'
import Icon from '@/components/icons/Icon.vue'
import { formatCostFixed, formatNumber, formatRelativeTime, formatTime, formatTokensK } from '@/utils/format'
import PlatformTypeBadge from '@/components/common/PlatformTypeBadge.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import UsageProgressBar from '@/components/account/UsageProgressBar.vue'
const props = defineProps<{ rows: PoolAccount[]; loading: boolean; failed: boolean; isAdmin: boolean }>()
defineEmits<{ requests: [account: PoolAccount] }>()
const isAdmin = computed(() => props.isAdmin)
const { t } = useI18n()
/** 窗口费用：管理员看账号侧成本，用户看自己实付。 */
function windowCost(w: PoolWindow | undefined): number {
  if (!w) return 0
  return isAdmin.value ? (w.cost ?? 0) : (w.user_cost ?? 0)
}

/** 容量列的并发占用配色。 */
function capacityClass(row: PoolAccount): string {
  const inUse = row.capacity?.in_use ?? row.in_use ?? 0
  const max = row.capacity?.concurrency ?? row.concurrency ?? 0
  if (max > 0 && inUse >= max) return 'text-red-600 dark:text-red-400'
  if (inUse > 0) return 'text-amber-600 dark:text-amber-400'
  return 'text-gray-500 dark:text-gray-400'
}

interface QuotaLike {
  utilization?: number
  resets_at?: string
  remaining_seconds?: number
  requests?: number
  tokens?: number
  cost?: number
  source?: string
  enabled?: boolean
}

interface QuotaWindowView {
  key: string
  label: string
  pctText: string
  pctClass: string
  barClass: string
  width: string
  detail: string
  resetText: string
}

/**
 * 单个用量窗口的展示读数。
 *
 * 进度条只认真数：没有可靠利用率时 pct 为 0、不画条、显示「—」，
 * 绝不编一个百分比出来。
 */
function quotaWindowView(w: QuotaLike | undefined, key: string, label: string): QuotaWindowView {
  const pct = Math.max(0, Math.min(100, w?.utilization ?? 0))
  const drawn = pct > 0
  let barClass = 'bg-emerald-500'
  let pctClass = 'text-emerald-600 dark:text-emerald-400'
  if (pct >= 90) {
    barClass = 'bg-red-500'
    pctClass = 'text-red-600 dark:text-red-400'
  } else if (pct >= 70) {
    barClass = 'bg-amber-500'
    pctClass = 'text-amber-600 dark:text-amber-400'
  }
  if (!drawn) pctClass = 'text-gray-400'
  const parts: string[] = []
  if (isAdmin.value && w) {
    if (w.requests != null) parts.push(t('admin.pool.extra.windowRequests', { n: formatNumber(w.requests) }))
    if (w.cost != null && w.cost > 0) parts.push(`$${formatCostFixed(w.cost)}`)
  }
  return {
    key,
    label,
    pctText: drawn ? `${pct.toFixed(pct >= 10 ? 0 : 1)}%` : '—',
    pctClass,
    barClass,
    width: `${pct}%`,
    detail: parts.join(' · '),
    resetText: w?.resets_at ? t('admin.pool.extra.windowResetsAt', { time: formatTime(w.resets_at) }) : ''
  }
}

function quotaWindows(row: PoolAccount): QuotaWindowView[] {
  return [
    quotaWindowView(row.five_hour, '5h', t('admin.pool.extra.window5h')),
    quotaWindowView(row.seven_day, '7d', t('admin.pool.extra.window7d'))
  ]
}

const accountColumns = computed<Column[]>(() => {
  const base: Column[] = [
    { key: 'account', label: t('admin.pool.columns.account') },
    { key: 'platform_type', label: t('admin.pool.columns.platformType') },
    { key: 'capacity', label: t('admin.pool.extra.capacity') },
    { key: 'status', label: t('admin.pool.columns.status') },
    { key: 'groups', label: t('admin.pool.columns.groups') },
    { key: 'usage_window', label: t('admin.pool.extra.usageWindow') }
  ]
  base.push(
    { key: 'today_requests', label: t('admin.pool.columns.todayRequests'), class: 'text-right' },
    { key: 'today_tokens', label: t('admin.pool.columns.todayTokens'), class: 'text-right' },
    { key: 'today_cost', label: t('admin.pool.columns.todayCost'), class: 'text-right' }
  )
  if (isAdmin.value) {
    base.push(
      { key: 'rate_multiplier', label: t('admin.pool.columns.rateMultiplier'), class: 'text-right' },
      { key: 'priority', label: t('admin.pool.columns.priority'), class: 'text-right' },
      { key: 'availability', label: t('admin.pool.columns.availability') }
    )
  }
  base.push(
    { key: 'last_used_at', label: t('admin.pool.columns.lastUsed') },
    { key: 'actions', label: t('admin.pool.columns.actions') }
  )
  return base
})

function statusLabel(status: string): string {
  if (status === 'active') return t('admin.pool.status.active')
  if (status === 'error') return t('admin.pool.status.error')
  if (status === 'disabled') return t('admin.pool.status.disabled')
  return status || '—'
}

function availabilityLabel(row: PoolAccount): string {
  if (row.rate_limit_reset_at) {
    return t('admin.pool.availability.rateLimited', { time: formatTime(row.rate_limit_reset_at) })
  }
  if (row.overload_until) {
    return t('admin.pool.availability.overload', { time: formatTime(row.overload_until) })
  }
  if (row.temp_unschedulable_until) {
    return t('admin.pool.availability.tempUnschedulable', {
      time: formatTime(row.temp_unschedulable_until)
    })
  }
  if (!row.schedulable) {
    return t('admin.pool.availability.notScheduled')
  }
  return t('admin.pool.availability.normal')
}

function availabilityTitle(row: PoolAccount): string {
  const parts = [availabilityLabel(row)]
  if (row.temp_unschedulable_reason) parts.push(row.temp_unschedulable_reason)
  if (row.session_window_status) parts.push(`window: ${row.session_window_status}`)
  return parts.join(' · ')
}

function availabilityClass(row: PoolAccount): string {
  if (row.rate_limit_reset_at || row.overload_until) return 'badge badge-warning'
  if (row.temp_unschedulable_until || !row.schedulable) return 'badge badge-gray'
  return 'badge badge-success'
}


</script>
<template>
<DataTable

          :columns="accountColumns"
          :data="rows"
          :loading="loading && !rows.length"
          row-key="id"
        >
          <template #cell-account="{ row }">
            <div class="min-w-0 max-w-[240px]">
              <div class="truncate font-medium text-gray-900 dark:text-white" :title="row.name">
                {{ row.name || `#${row.id}` }}
              </div>
              <div v-if="isAdmin && row.notes" class="mt-0.5 flex items-center gap-1.5 text-xs text-gray-400">
                <span class="truncate" :title="row.notes">{{ row.notes }}</span>
              </div>
            </div>
          </template>

          <template #cell-platform_type="{ row }">
            <PlatformTypeBadge :platform="row.platform" :type="row.type" />
          </template>

          <template #cell-status="{ row }">
            <div class="flex flex-col gap-1">
              <StatusBadge :status="row.status" :label="statusLabel(row.status)" />
              <span
                v-if="isAdmin && row.error_message"
                class="max-w-[200px] truncate text-xs text-red-500"
                :title="row.error_message"
              >
                {{ row.error_message }}
              </span>
            </div>
          </template>

          <template #cell-groups="{ row }">
            <div v-if="row.groups && row.groups.length" class="flex max-w-[200px] flex-wrap gap-1">
              <span v-for="g in row.groups" :key="g.id" class="badge badge-gray">{{ g.name }}</span>
            </div>
            <span v-else class="text-gray-400">—</span>
          </template>

          <template #cell-capacity="{ row }">
            <div class="flex flex-col gap-1">
              <div class="flex items-center gap-1 whitespace-nowrap font-mono text-xs tabular-nums">
                <span :class="capacityClass(row)">{{ row.capacity?.in_use ?? row.in_use ?? 0 }}</span>
                <span class="text-gray-400">/</span>
                <span class="text-gray-600 dark:text-gray-300">{{ row.capacity?.concurrency ?? row.concurrency ?? 0 }}</span>
              </div>
              <div v-if="isAdmin" class="flex flex-wrap items-center gap-1">
                <span
                  v-if="row.capacity?.supports_window_cost && (row.capacity?.window_cost_limit ?? 0) > 0"
                  class="inline-flex items-center rounded px-1 py-0.5 font-mono text-[11px] text-gray-500 dark:text-gray-400"
                  :title="t('admin.pool.extra.windowCostLimitHint')"
                >
                  ${{ formatCostFixed(row.capacity?.window_cost_used ?? 0) }}/${{ formatCostFixed(row.capacity?.window_cost_limit ?? 0) }}
                </span>
                <span
                  v-if="row.capacity?.supports_quota && (row.capacity?.quota_weekly_limit ?? 0) > 0"
                  class="inline-flex items-center rounded px-1 py-0.5 font-mono text-[11px] text-gray-500 dark:text-gray-400"
                >
                  W {{ formatNumber(row.capacity?.quota_weekly_used ?? 0) }}/{{ formatNumber(row.capacity?.quota_weekly_limit ?? 0) }}
                </span>
                <span
                  v-if="row.capacity?.supports_quota && (row.capacity?.quota_daily_limit ?? 0) > 0"
                  class="inline-flex items-center rounded px-1 py-0.5 font-mono text-[11px] text-gray-500 dark:text-gray-400"
                >
                  D {{ formatNumber(row.capacity?.quota_daily_used ?? 0) }}/{{ formatNumber(row.capacity?.quota_daily_limit ?? 0) }}
                </span>
                <span
                  v-if="row.capacity?.supports_quota && (row.capacity?.quota_limit ?? 0) > 0"
                  class="inline-flex items-center rounded px-1 py-0.5 font-mono text-[11px] text-gray-500 dark:text-gray-400"
                >
                  T {{ formatNumber(row.capacity?.quota_used ?? 0) }}/{{ formatNumber(row.capacity?.quota_limit ?? 0) }}
                </span>
              </div>
              <span
                v-if="row.capacity?.quota_warning"
                class="inline-flex w-fit items-center gap-1 rounded bg-red-50 px-1.5 py-0.5 text-[10px] font-medium text-red-600 dark:bg-red-900/30 dark:text-red-400"
                :title="t('admin.pool.extra.quotaWarning.' + row.capacity.quota_warning)"
              >
                <svg class="h-3 w-3 shrink-0" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
                  <path fill-rule="evenodd" d="M8.485 2.495c.673-1.167 2.357-1.167 3.03 0l6.28 10.875c.673 1.167-.17 2.625-1.516 2.625H3.72c-1.347 0-2.189-1.458-1.515-2.625L8.485 2.495ZM10 6a.75.75 0 0 1 .75.75v3.5a.75.75 0 0 1-1.5 0v-3.5A.75.75 0 0 1 10 6Zm0 9a1 1 0 1 0 0-2 1 1 0 0 0 0 2Z" clip-rule="evenodd" />
                </svg>
                {{ t('admin.pool.extra.quotaWarningShort') }}
              </span>
            </div>
          </template>

          <template #cell-usage_window="{ row }">
            <div class="space-y-1">
              <div v-for="w in quotaWindows(row)" :key="w.key" :title="[w.detail,w.resetText].filter(Boolean).join(' · ')">
                <UsageProgressBar v-if="w.pctText!=='—'" :label="w.key" :utilization="Number(w.width.replace('%',''))" :resets-at="w.key==='5h'?row.five_hour?.resets_at:row.seven_day?.resets_at" :color="w.key==='5h'?'indigo':'emerald'" />
                <span v-else class="text-xs text-gray-400">{{ w.key }} —</span>
              </div>
            </div>
          </template>

          <template #cell-today_requests="{ row }">
            <span class="font-mono tabular-nums text-gray-800 dark:text-gray-200">
              {{ formatNumber(row.today?.requests ?? 0) }}
            </span>
          </template>

          <template #cell-today_tokens="{ row }">
            <span class="whitespace-nowrap font-mono tabular-nums text-gray-600 dark:text-gray-300">
              {{ formatTokensK(row.today?.tokens ?? 0) }}
            </span>
          </template>

          <template #cell-today_cost="{ row }">
            <span class="whitespace-nowrap font-mono tabular-nums text-gray-800 dark:text-gray-200">
              {{ formatCostFixed(windowCost(row.today)) }}
            </span>
          </template>

          <template #cell-concurrency="{ row }">
            <span
              class="whitespace-nowrap font-mono tabular-nums"
              :class="
                (row.in_use ?? 0) > 0
                  ? 'text-primary-600 dark:text-primary-400'
                  : 'text-gray-500 dark:text-gray-400'
              "
            >
              {{ row.in_use ?? 0 }} / {{ row.load_factor || row.concurrency || 0 }}
            </span>
          </template>

          <template #cell-rate_multiplier="{ value }">
            <span class="font-mono tabular-nums text-gray-600 dark:text-gray-300">
              {{ Number(value ?? 1).toFixed(2) }}×
            </span>
          </template>

          <template #cell-priority="{ value }">
            <span class="font-mono tabular-nums text-gray-600 dark:text-gray-300">{{ value }}</span>
          </template>

          <template #cell-last_used_at="{ value }">
            <span class="whitespace-nowrap text-gray-600 dark:text-gray-300">
              {{ value ? formatRelativeTime(value) : t('admin.pool.availability.never') }}
            </span>
          </template>

          <template #cell-availability="{ row }">
            <span :class="availabilityClass(row)" :title="availabilityTitle(row)">
              {{ availabilityLabel(row) }}
            </span>
          </template>

          <template #cell-actions="{ row }">
            <span
              v-if="row.synthetic"
              class="inline-flex cursor-not-allowed items-center gap-1 whitespace-nowrap font-medium text-gray-400 dark:text-dark-500"
              :title="t('admin.pool.extra.synthetic.noRequests')"
            >
              <Icon name="eye" size="sm" />
              {{ t('admin.pool.actions.requests') }}
            </span>
            <button
              v-else
              type="button"
              class="inline-flex items-center gap-1 whitespace-nowrap font-medium text-primary-600 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
              @click="$emit('requests', row)"
            >
              <Icon name="eye" size="sm" />
              {{ t('admin.pool.actions.requests') }}
            </button>
          </template>

          <template #empty>
            <div class="flex flex-col items-center py-8">
              <Icon name="server" size="xl" class="mb-4 h-12 w-12 text-gray-300 dark:text-dark-600" />
              <p class="text-sm font-medium text-gray-500 dark:text-gray-400">
                {{ failed ? t('admin.pool.networkError') : isAdmin ? t('admin.pool.empty') : t('admin.pool.user.empty') }}
              </p>
            </div>
          </template>
</DataTable>
</template>
