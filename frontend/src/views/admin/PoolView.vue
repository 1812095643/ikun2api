<template>
  <AppLayout>
    <TablePageLayout>
      <!-- 固定区：标签切换 + 概览 + 筛选 -->
      <template #filters>
        <div class="space-y-4">
          <!-- 标签栏：账号池 / 使用记录 在同一个页面内切换 -->
          <div class="card px-2 py-2">
            <div class="flex flex-wrap items-center gap-1" role="tablist">
              <button
                v-for="tab in tabItems"
                :key="tab.key"
                type="button"
                role="tab"
                :aria-selected="activeTab === tab.key"
                class="inline-flex items-center gap-2 rounded-xl px-4 py-2 text-sm font-medium transition-colors"
                :class="
                  activeTab === tab.key
                    ? 'bg-primary-50 text-primary-700 dark:bg-primary-500/10 dark:text-primary-300'
                    : 'text-gray-500 hover:bg-gray-50 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-700 dark:hover:text-dark-200'
                "
                @click="switchTab(tab.key)"
              >
                <Icon :name="tab.icon" size="sm" />
                {{ tab.label }}
              </button>
            </div>
          </div>

          <!-- ==================== 账号池 ==================== -->
          <template v-if="activeTab === 'accounts'">
            <!-- 概览条 -->
            <div class="card p-4 sm:p-6">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <div class="min-w-0">
                  <h2 class="text-base font-semibold text-gray-900 dark:text-white">
                    {{ isAdmin ? t('admin.pool.title') : t('admin.pool.user.title') }}
                  </h2>
                  <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                    {{ isAdmin ? t('admin.pool.description') : t('admin.pool.user.description') }}
                  </p>
                </div>
                <div class="flex items-center gap-3">
                  <span v-if="overview" class="whitespace-nowrap text-xs text-gray-400 dark:text-dark-500">
                    {{ formatTime(overview.generated_at) }}
                  </span>
                  <button
                    type="button"
                    class="btn btn-secondary"
                    :disabled="accountsLoading"
                    :title="t('common.refresh')"
                    @click="refreshAccounts"
                  >
                    <Icon name="refresh" size="md" :class="accountsLoading ? 'animate-spin' : ''" />
                  </button>
                </div>
              </div>

              <div class="mt-4 grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6">
                <div v-for="item in overviewCards" :key="item.key" class="min-w-0">
                  <p
                    class="truncate text-xs font-medium uppercase tracking-wider text-gray-400 dark:text-dark-500"
                  >
                    {{ item.label }}
                  </p>
                  <p
                    class="mt-1 truncate text-xl font-semibold text-gray-900 dark:text-white"
                    :title="item.value"
                  >
                    {{ item.value }}
                  </p>
                  <p class="mt-0.5 truncate text-xs text-gray-400 dark:text-dark-500" :title="item.hint">
                    {{ item.hint }}
                  </p>
                </div>
              </div>
            </div>

            <!-- 筛选行 -->
            <div class="card p-4 sm:p-6">
              <div class="flex flex-wrap items-end justify-between gap-4">
                <div class="flex flex-1 flex-wrap items-end gap-4">
                  <div class="w-full sm:w-auto sm:min-w-[240px]">
                    <label class="input-label">{{ t('admin.pool.filters.search') }}</label>
                    <div class="relative">
                      <Icon
                        name="search"
                        size="md"
                        class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"
                      />
                      <input
                        v-model.trim="accountFilters.search"
                        type="text"
                        class="input pl-10"
                        :placeholder="t('admin.pool.filters.searchPlaceholder')"
                        @keyup.enter="searchAccounts"
                      />
                    </div>
                  </div>

                  <div class="w-full sm:w-auto sm:min-w-[180px]">
                    <label class="input-label">{{ t('admin.pool.filters.platform') }}</label>
                    <Select
                      v-model="accountFilters.platform"
                      :options="platformOptions"
                      @change="searchAccounts"
                    />
                  </div>

                  <div class="w-full sm:w-auto sm:min-w-[160px]">
                    <label class="input-label">{{ t('admin.pool.filters.type') }}</label>
                    <Select v-model="accountFilters.type" :options="typeOptions" @change="searchAccounts" />
                  </div>

                  <div class="w-full sm:w-auto sm:min-w-[160px]">
                    <label class="input-label">{{ t('admin.pool.filters.status') }}</label>
                    <Select
                      v-model="accountFilters.status"
                      :options="statusOptions"
                      @change="searchAccounts"
                    />
                  </div>

                  <div v-if="isAdmin" class="w-full sm:w-auto sm:min-w-[170px]">
                    <label class="input-label">{{ t('admin.pool.filters.group') }}</label>
                    <Select
                      v-model="accountFilters.group_id"
                      :options="groupOptions"
                      @change="searchAccounts"
                    />
                  </div>
                </div>

                <div class="flex w-full flex-wrap items-center justify-end gap-3 sm:w-auto">
                  <button
                    type="button"
                    class="btn btn-primary"
                    :disabled="accountsLoading"
                    @click="searchAccounts"
                  >
                    {{ t('common.search') }}
                  </button>
                  <button
                    type="button"
                    class="btn btn-secondary"
                    :disabled="accountsLoading"
                    @click="resetAccountFilters"
                  >
                    {{ t('common.reset') }}
                  </button>
                </div>
              </div>
            </div>
          </template>

          <!-- ==================== 使用记录 ==================== -->
          <template v-else>
            <div class="card p-4 sm:p-6">
              <div class="mb-4 min-w-0">
                <h2 class="text-base font-semibold text-gray-900 dark:text-white">
                  {{ isAdmin ? t('admin.pool.usage.title') : t('admin.pool.usage.userTitle') }}
                </h2>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                  {{ isAdmin ? t('admin.pool.usage.description') : t('admin.pool.usage.userDescription') }}
                </p>
              </div>

              <div class="flex flex-wrap items-end justify-between gap-4">
                <div class="flex flex-1 flex-wrap items-end gap-4">
                  <div class="w-full sm:w-auto sm:min-w-[260px]">
                    <label class="input-label">{{ t('admin.dashboard.timeRange') }}</label>
                    <DateRangePicker
                      v-model:start-date="usageStartDate"
                      v-model:end-date="usageEndDate"
                      @change="onDateRangeChange"
                    />
                  </div>

                  <div class="w-full sm:w-auto sm:min-w-[170px]">
                    <label class="input-label">{{ t('admin.pool.usage.filters.accountId') }}</label>
                    <input
                      v-model.trim="usageFilters.account_id"
                      type="text"
                      inputmode="numeric"
                      class="input"
                      :placeholder="t('admin.pool.usage.filters.accountPlaceholder')"
                      @keyup.enter="searchUsage"
                    />
                  </div>

                  <div v-if="isAdmin" class="w-full sm:w-auto sm:min-w-[170px]">
                    <label class="input-label">{{ t('admin.pool.usage.filters.userId') }}</label>
                    <input
                      v-model.trim="usageFilters.user_id"
                      type="text"
                      inputmode="numeric"
                      class="input"
                      :placeholder="t('admin.pool.usage.filters.accountPlaceholder')"
                      @keyup.enter="searchUsage"
                    />
                  </div>

                  <div class="w-full sm:w-auto sm:min-w-[170px]">
                    <label class="input-label">{{ t('admin.pool.usage.filters.apiKeyId') }}</label>
                    <input
                      v-model.trim="usageFilters.api_key_id"
                      type="text"
                      inputmode="numeric"
                      class="input"
                      :placeholder="t('admin.pool.usage.filters.accountPlaceholder')"
                      @keyup.enter="searchUsage"
                    />
                  </div>

                  <div class="w-full sm:w-auto sm:min-w-[200px]">
                    <label class="input-label">{{ t('admin.pool.usage.filters.model') }}</label>
                    <input
                      v-model.trim="usageFilters.model"
                      type="text"
                      class="input"
                      :placeholder="t('admin.pool.usage.filters.modelPlaceholder')"
                      @keyup.enter="searchUsage"
                    />
                  </div>

                  <div class="w-full sm:w-auto sm:min-w-[200px]">
                    <label class="input-label">{{ t('admin.pool.usage.filters.requestId') }}</label>
                    <input
                      v-model.trim="usageFilters.request_id"
                      type="text"
                      class="input"
                      :placeholder="t('admin.pool.usage.filters.requestIdPlaceholder')"
                      @keyup.enter="searchUsage"
                    />
                  </div>
                </div>

                <div class="flex w-full flex-wrap items-center justify-end gap-3 sm:w-auto">
                  <button
                    type="button"
                    class="btn btn-primary"
                    :disabled="usageLoading"
                    @click="searchUsage"
                  >
                    {{ t('common.search') }}
                  </button>
                  <button
                    type="button"
                    class="btn btn-secondary"
                    :disabled="usageLoading"
                    @click="resetUsageFilters"
                  >
                    {{ t('common.reset') }}
                  </button>
                </div>
              </div>
            </div>
          </template>
        </div>
      </template>

      <!-- 表格区 -->
      <template #table>
        <!-- 账号池表格 -->
        <DataTable
          v-if="activeTab === 'accounts'"
          :columns="accountColumns"
          :data="accounts"
          :loading="accountsLoading"
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
            <div class="flex min-w-[168px] flex-col gap-1.5">
              <div v-for="w in quotaWindows(row)" :key="w.key" class="flex flex-col gap-0.5">
                <div class="flex items-center justify-between text-xs">
                  <span class="text-gray-500 dark:text-gray-400">{{ w.label }}</span>
                  <span class="font-mono tabular-nums" :class="w.pctClass">{{ w.pctText }}</span>
                </div>
                <div class="h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-gray-700">
                  <div class="h-full rounded-full transition-all" :class="w.barClass" :style="{ width: w.width }" />
                </div>
                <div class="flex items-center justify-between gap-2 text-[11px] text-gray-400">
                  <span class="truncate font-mono" :title="w.detail">{{ w.detail }}</span>
                  <span v-if="w.resetText" class="whitespace-nowrap">{{ w.resetText }}</span>
                </div>
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
            <button
              type="button"
              class="inline-flex items-center gap-1 whitespace-nowrap font-medium text-primary-600 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
              @click="openRequests(row)"
            >
              <Icon name="eye" size="sm" />
              {{ t('admin.pool.actions.requests') }}
            </button>
          </template>

          <template #empty>
            <div class="flex flex-col items-center py-8">
              <Icon name="server" size="xl" class="mb-4 h-12 w-12 text-gray-300 dark:text-dark-600" />
              <p class="text-sm font-medium text-gray-500 dark:text-gray-400">
                {{ isAdmin ? t('admin.pool.empty') : t('admin.pool.user.empty') }}
              </p>
            </div>
          </template>
        </DataTable>

        <!-- 使用记录表格 -->
        <DataTable
          v-else
          :columns="usageColumns"
          :data="usageRows"
          :loading="usageLoading"
          row-key="id"
        >
          <template #cell-created_at="{ value }">
            <span class="whitespace-nowrap text-gray-600 dark:text-gray-300">{{ formatTime(value) }}</span>
          </template>

          <template #cell-account="{ row }">
            <div class="min-w-0 max-w-[200px]">
              <div class="truncate font-medium text-gray-900 dark:text-white" :title="accountLabel(row)">
                {{ accountLabel(row) }}
              </div>
            </div>
          </template>

          <template #cell-platform="{ row }">
            <span v-if="row.account_platform_snapshot" class="badge badge-gray">
              {{ row.account_platform_snapshot }}
            </span>
            <span v-else class="text-gray-400">{{ t('admin.pool.usage.noSnapshot') }}</span>
          </template>

          <template #cell-model="{ row }">
            <div class="min-w-0 max-w-[200px]">
              <div class="truncate text-gray-800 dark:text-gray-200" :title="row.model">{{ row.model }}</div>
              <div
                v-if="row.model_mapping_chain"
                class="mt-0.5 truncate font-mono text-xs text-gray-400"
                :title="row.model_mapping_chain"
              >
                {{ row.model_mapping_chain }}
              </div>
            </div>
          </template>

          <template #cell-upstream_model="{ row }">
            <span
              class="max-w-[180px] truncate font-mono text-xs"
              :class="
                row.upstream_model_mismatch
                  ? 'text-amber-600 dark:text-amber-400'
                  : 'text-gray-500 dark:text-gray-400'
              "
              :title="row.upstream_model || ''"
            >
              {{ row.upstream_model || '—' }}
            </span>
          </template>

          <template #cell-endpoint="{ row }">
            <span
              class="max-w-[180px] truncate font-mono text-xs text-gray-500 dark:text-gray-400"
              :title="row.inbound_endpoint || row.upstream_endpoint || ''"
            >
              {{ row.inbound_endpoint || row.upstream_endpoint || '—' }}
            </span>
          </template>

          <template #cell-reasoning_effort="{ row }">
            <span
              v-if="row.reasoning_effort"
              class="inline-flex items-center rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-600 dark:bg-gray-700 dark:text-gray-300"
            >{{ row.reasoning_effort }}</span>
            <span v-else class="text-gray-400">—</span>
          </template>

          <template #cell-group_name="{ row }">
            <span
              v-if="row.group?.name"
              class="inline-flex items-center rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-600 dark:bg-gray-700 dark:text-gray-300"
            >{{ row.group.name }}</span>
            <span v-else class="text-gray-400">—</span>
          </template>

          <template #cell-stream_type="{ row }">
            <span class="text-xs text-gray-600 dark:text-gray-300">
              {{ row.stream ? t('admin.pool.extra.streaming') : t('admin.pool.extra.nonStreaming') }}
            </span>
          </template>

          <template #cell-user_agent="{ row }">
            <span
              class="inline-block max-w-[150px] truncate align-middle font-mono text-xs text-gray-500 dark:text-gray-400"
              :title="row.user_agent || ''"
            >{{ row.user_agent || '—' }}</span>
          </template>

          <template #cell-ip_address="{ row }">
            <span class="whitespace-nowrap font-mono text-xs text-gray-500 dark:text-gray-400">{{ row.ip_address || '—' }}</span>
          </template>

          <template #cell-user="{ row }">
            <div class="min-w-0 max-w-[160px]">
              <div class="truncate text-gray-800 dark:text-gray-200" :title="row.user?.email || ''">
                {{ row.user?.email || row.user?.username || `#${row.user_id}` }}
              </div>
              <div class="mt-0.5 truncate font-mono text-xs text-gray-400">#{{ row.user_id }}</div>
            </div>
          </template>

          <template #cell-api_key="{ row }">
            <span
              class="max-w-[140px] truncate font-mono text-xs text-gray-500 dark:text-gray-400"
              :title="row.api_key?.name || ''"
            >
              {{ row.api_key?.name || `#${row.api_key_id}` }}
            </span>
          </template>

          <template #cell-tokens="{ row }">
            <span class="whitespace-nowrap font-mono tabular-nums text-gray-600 dark:text-gray-300">
              {{ formatNumber(row.input_tokens) }} / {{ formatNumber(row.output_tokens) }}
            </span>
          </template>

          <template #cell-cost="{ row }">
            <span class="whitespace-nowrap font-mono tabular-nums text-gray-800 dark:text-gray-200">
              {{ formatCostFixed(rowCost(row)) }}
            </span>
          </template>

          <template #cell-duration="{ row }">
            <span class="whitespace-nowrap text-gray-500 dark:text-gray-400">
              {{ row.duration_ms != null ? `${row.duration_ms} ms` : '—' }}
            </span>
          </template>

          <template #cell-request_id="{ value }">
            <span class="max-w-[180px] truncate font-mono text-xs text-gray-400" :title="value">{{ value }}</span>
          </template>

          <template #empty>
            <div class="flex flex-col items-center py-8">
              <Icon name="chart" size="xl" class="mb-4 h-12 w-12 text-gray-300 dark:text-dark-600" />
              <p class="text-sm font-medium text-gray-500 dark:text-gray-400">
                {{ t('admin.pool.usage.empty') }}
              </p>
            </div>
          </template>
        </DataTable>
      </template>

      <!-- 分页 -->
      <template #pagination>
        <Pagination
          v-if="activeTab === 'accounts' && accountsTotal > 0"
          :total="accountsTotal"
          :page="accountsPage"
          :page-size="accountsPageSize"
          @update:page="onAccountsPageChange"
          @update:pageSize="onAccountsPageSizeChange"
        />
        <Pagination
          v-else-if="activeTab === 'usage' && usageTotal > 0"
          :total="usageTotal"
          :page="usagePage"
          :page-size="usagePageSize"
          @update:page="onUsagePageChange"
          @update:pageSize="onUsagePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <!-- 账号请求流水抽屉 -->
    <BaseDialog
      :show="requestsVisible"
      :title="t('admin.pool.dialog.title', { id: activeAccount?.id ?? '' })"
      width="wide"
      :close-on-click-outside="true"
      @close="closeRequests"
    >
      <div class="space-y-4">
        <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.pool.dialog.subtitle') }}</p>

        <div v-if="requestsLoading" class="flex items-center justify-center py-12">
          <div class="flex flex-col items-center gap-3">
            <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"></div>
            <div class="text-sm font-medium text-gray-500 dark:text-gray-400">
              {{ t('common.loading') }}
            </div>
          </div>
        </div>

        <div v-else-if="!requestRows.length" class="flex flex-col items-center py-12">
          <Icon name="inbox" size="xl" class="mb-4 h-12 w-12 text-gray-300 dark:text-dark-600" />
          <p class="text-sm font-medium text-gray-500 dark:text-gray-400">
            {{ t('admin.pool.dialog.empty') }}
          </p>
        </div>

        <div v-else class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <thead>
              <tr class="text-left text-xs font-semibold uppercase tracking-wider text-gray-400">
                <th class="whitespace-nowrap py-2 pr-4">{{ t('admin.pool.dialog.columns.time') }}</th>
                <th class="whitespace-nowrap py-2 pr-4">{{ t('admin.pool.dialog.columns.model') }}</th>
                <th class="whitespace-nowrap py-2 pr-4">{{ t('admin.pool.dialog.columns.endpoint') }}</th>
                <th class="whitespace-nowrap py-2 pr-4 text-right">
                  {{ t('admin.pool.dialog.columns.tokens') }}
                </th>
                <th class="whitespace-nowrap py-2 pr-4 text-right">
                  {{ t('admin.pool.dialog.columns.cost') }}
                </th>
                <th class="whitespace-nowrap py-2 pr-4 text-right">
                  {{ t('admin.pool.dialog.columns.duration') }}
                </th>
                <th class="whitespace-nowrap py-2">{{ t('admin.pool.dialog.columns.requestId') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
              <tr v-for="row in requestRows" :key="row.id">
                <td class="whitespace-nowrap py-2 pr-4 text-gray-600 dark:text-gray-300">
                  {{ formatTime(row.created_at) }}
                </td>
                <td class="max-w-[180px] truncate py-2 pr-4 text-gray-800 dark:text-gray-200" :title="row.model">
                  {{ row.model }}
                </td>
                <td
                  class="max-w-[160px] truncate py-2 pr-4 font-mono text-xs text-gray-500"
                  :title="row.inbound_endpoint || ''"
                >
                  {{ row.inbound_endpoint || '—' }}
                </td>
                <td
                  class="whitespace-nowrap py-2 pr-4 text-right font-mono tabular-nums text-gray-600 dark:text-gray-300"
                >
                  {{ formatNumber(row.input_tokens) }} / {{ formatNumber(row.output_tokens) }}
                </td>
                <td
                  class="whitespace-nowrap py-2 pr-4 text-right font-mono tabular-nums text-gray-800 dark:text-gray-200"
                >
                  {{ formatCostFixed(rowCost(row)) }}
                </td>
                <td class="whitespace-nowrap py-2 pr-4 text-right font-mono tabular-nums text-gray-500">
                  {{ row.duration_ms != null ? `${row.duration_ms} ms` : '—' }}
                </td>
                <td class="max-w-[200px] truncate py-2 font-mono text-xs text-gray-400" :title="row.request_id">
                  {{ row.request_id }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-if="requestsTotal > requestPageSize" class="flex justify-end">
          <Pagination
            :total="requestsTotal"
            :page="requestPage"
            :page-size="requestPageSize"
            :show-page-size-selector="false"
            @update:page="onRequestPageChange"
          />
        </div>
      </div>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import type { AdminUsageLog, PaginatedResponse, UsageLog } from '@/types'
import {
  getPoolOverview,
  listPoolAccountRequests,
  listPoolAccounts,
  listPoolUsage,
  type PoolAccount,
  type PoolOverview,
  type PoolScope,
  type PoolUsageQuery,
  type PoolUserOverview,
  type PoolWindow
} from '@/api/pool'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import type { Column } from '@/components/common/types'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PlatformTypeBadge from '@/components/common/PlatformTypeBadge.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import { getPersistedPageSize, setPersistedPageSize } from '@/composables/usePersistedPageSize'
import {
  formatCostFixed,
  formatNumber,
  formatRelativeTime,
  formatTime,
  formatTokensK
} from '@/utils/format'
import { useAppStore, useAuthStore } from '@/stores'
import { getAll as listAdminGroups } from '@/api/admin/groups'

type PoolTab = 'accounts' | 'usage'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()

/**
 * 同一个页面、同一个组件，按身份决定数据源与可见列：
 *   - 管理员 → /admin/pool/*，全量字段（分组、并发、倍率、优先级、可用性、错误原因）；
 *   - 普通用户 → /pool/*，服务端强制只返回自己的记录，且已剔除内部调度字段。
 */
const isAdmin = computed(() => authStore.isAdmin)
const scope = computed<PoolScope>(() => (isAdmin.value ? 'admin' : 'user'))

const initialTab: PoolTab = route.query.tab === 'usage' ? 'usage' : 'accounts'
const activeTab = ref<PoolTab>(initialTab)

interface PoolTabItem {
  key: PoolTab
  label: string
  icon: 'server' | 'chart'
}

const tabItems = computed<PoolTabItem[]>(() => [
  { key: 'accounts', label: t('admin.pool.navAccounts'), icon: 'server' },
  { key: 'usage', label: t('admin.pool.navUsage'), icon: 'chart' }
])

function switchTab(tab: PoolTab) {
  if (activeTab.value === tab) return
  activeTab.value = tab
  router.replace({ query: { ...route.query, tab } })
  if (tab === 'usage' && !usageLoaded) fetchUsage()
}

// ==================== 账号池 ====================

const accountsLoading = ref(false)
const accounts = ref<PoolAccount[]>([])
const overview = ref<(PoolOverview & PoolUserOverview) | null>(null)
const accountsTotal = ref(0)
const accountsPage = ref(1)
const accountsPageSize = ref(getPersistedPageSize(20))

const accountFilters = reactive({
  search: '',
  platform: '',
  type: '',
  status: '',
  group_id: ''
})

// 分组下拉：只给管理员用，普通用户看不到任何分组维度。
const groupOptions = ref<{ value: string; label: string }[]>([])

const platformOptions = computed(() => [
  { value: '', label: t('admin.pool.filters.allPlatform') },
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'openai_bps', label: 'OpenAI BPS' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' },
  { value: 'grok', label: 'Grok' },
  { value: 'kimi', label: 'Kimi' },
  { value: 'zhipu', label: '智谱 GLM' },
  { value: 'deepseek', label: 'DeepSeek' },
  { value: 'minimax', label: 'MiniMax' },
  { value: 'typesafe', label: 'TypeSafe' },
  { value: 'opencode_go', label: 'OpenCode' },
  { value: 'command_code', label: 'Command Code' }
])

const typeOptions = computed(() => [
  { value: '', label: t('admin.pool.filters.allType') },
  { value: 'oauth', label: 'OAuth' },
  { value: 'setup-token', label: 'Setup Token' },
  { value: 'apikey', label: 'API Key' },
  { value: 'upstream', label: 'Upstream' },
  { value: 'bedrock', label: 'Bedrock' },
  { value: 'service_account', label: 'Service Account' }
])

const statusOptions = computed(() => [
  { value: '', label: t('admin.pool.filters.allStatus') },
  { value: 'active', label: t('admin.pool.status.active') },
  { value: 'error', label: t('admin.pool.status.error') },
  { value: 'disabled', label: t('admin.pool.status.disabled') }
])

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
    resetText: w?.resets_at ? t('admin.pool.extra.windowResetsAt', { time: formatRelativeTime(w.resets_at) }) : ''
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

interface OverviewCard {
  key: string
  label: string
  value: string
  hint: string
}

const overviewCards = computed<OverviewCard[]>(() => {
  const o = overview.value
  const today = o?.today
  const week = o?.window_7d

  if (!isAdmin.value) {
    const accountsCount = o?.account_count ?? 0
    const activeCount = o?.active_count ?? 0
    return [
      {
        key: 'accounts',
        label: t('admin.pool.user.overview.accounts'),
        value: formatNumber(accountsCount),
        hint: t('admin.pool.user.overview.accountsHint', { active: activeCount })
      },
      {
        key: 'today_requests',
        label: t('admin.pool.overview.todayRequests'),
        value: formatNumber(today?.requests ?? 0),
        hint: formatTokensK(today?.tokens ?? 0)
      },
      {
        key: 'today_cost',
        label: t('admin.pool.overview.todayCost'),
        value: formatCostFixed(today?.user_cost ?? 0),
        hint: formatCostFixed(today?.standard_cost ?? 0)
      },
      {
        key: 'week_requests',
        label: t('admin.pool.overview.weekRequests'),
        value: formatNumber(week?.requests ?? 0),
        hint: t('admin.pool.overview.weekRequestsHint')
      }
    ]
  }

  return [
    {
      key: 'accounts',
      label: t('admin.pool.overview.accounts'),
      value: formatNumber(o?.total_accounts ?? 0),
      hint: t('admin.pool.overview.activeHint', {
        active: o?.active_accounts ?? 0,
        error: o?.error_accounts ?? 0,
        disabled: o?.disabled_accounts ?? 0
      })
    },
    {
      key: 'schedulable',
      label: t('admin.pool.overview.schedulable'),
      value: formatNumber(o?.schedulable_count ?? 0),
      hint: t('admin.pool.overview.schedulableHint')
    },
    {
      key: 'in_use',
      label: t('admin.pool.overview.inUse'),
      value: formatNumber(o?.total_in_use ?? 0),
      hint: t('admin.pool.overview.inUseHint')
    },
    {
      key: 'today_requests',
      label: t('admin.pool.overview.todayRequests'),
      value: formatNumber(today?.requests ?? 0),
      hint: formatTokensK(today?.tokens ?? 0)
    },
    {
      key: 'today_cost',
      label: t('admin.pool.overview.todayCost'),
      value: formatCostFixed(today?.cost ?? 0),
      hint: formatCostFixed(today?.standard_cost ?? 0)
    },
    {
      key: 'week_requests',
      label: t('admin.pool.overview.weekRequests'),
      value: formatNumber(week?.requests ?? 0),
      hint: t('admin.pool.overview.weekRequestsHint')
    }
  ]
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

let accountsAbort: AbortController | null = null

async function fetchAccounts() {
  accountsAbort?.abort()
  accountsAbort = new AbortController()
  accountsLoading.value = true
  try {
    const params: Record<string, unknown> = {
      page: accountsPage.value,
      page_size: accountsPageSize.value
    }
    if (accountFilters.search) params.search = accountFilters.search
    if (accountFilters.platform) params.platform = accountFilters.platform
    if (accountFilters.type) params.type = accountFilters.type
    if (accountFilters.status) params.status = accountFilters.status
    if (accountFilters.group_id) params.group_id = accountFilters.group_id

    const result: PaginatedResponse<PoolAccount> = await listPoolAccounts(
      params,
      scope.value,
      { signal: accountsAbort.signal }
    )
    accounts.value = result.items ?? []
    accountsTotal.value = result.total ?? 0
  } catch (error) {
    if ((error as { name?: string })?.name === 'CanceledError') return
    accounts.value = []
    accountsTotal.value = 0
    appStore.showError(t('admin.pool.loadFailed'))
  } finally {
    accountsLoading.value = false
  }
}

async function fetchOverview() {
  try {
    overview.value = await getPoolOverview(scope.value)
  } catch {
    // 概览失败不阻塞列表：保留上一份数据即可。
    appStore.showError(t('admin.pool.overviewFailed'))
  }
}

async function refreshAccounts() {
  await Promise.all([fetchAccounts(), fetchOverview()])
}

function searchAccounts() {
  accountsPage.value = 1
  fetchAccounts()
}

function resetAccountFilters() {
  accountFilters.search = ''
  accountFilters.platform = ''
  accountFilters.type = ''
  accountFilters.status = ''
  accountFilters.group_id = ''
  searchAccounts()
}

function onAccountsPageChange(p: number) {
  accountsPage.value = p
  fetchAccounts()
}

function onAccountsPageSizeChange(ps: number) {
  accountsPageSize.value = ps
  accountsPage.value = 1
  setPersistedPageSize(ps)
  fetchAccounts()
}

// ==================== 使用记录 ====================

const usageLoading = ref(false)
const usageLoaded = ref(false)
const usageRows = ref<(AdminUsageLog & UsageLog)[]>([])
const usageTotal = ref(0)
const usagePage = ref(1)
const usagePageSize = ref(getPersistedPageSize(20))

const formatLocalDate = (d: Date): string => {
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const defaultRange = (() => {
  const end = new Date()
  const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
  return { start: formatLocalDate(start), end: formatLocalDate(end) }
})()

const usageStartDate = ref(defaultRange.start)
const usageEndDate = ref(defaultRange.end)

const usageFilters = reactive({
  account_id: '',
  user_id: '',
  api_key_id: '',
  model: '',
  request_id: ''
})

const usageColumns = computed<Column[]>(() => {
  const base: Column[] = []
  if (isAdmin.value) {
    base.push({ key: 'user', label: t('admin.pool.usage.columns.user') })
  }
  base.push(
    { key: 'account', label: t('admin.pool.usage.columns.account') },
    { key: 'model', label: t('admin.pool.usage.columns.model') },
    { key: 'reasoning_effort', label: t('admin.pool.extra.reasoningEffort') },
    { key: 'endpoint', label: t('admin.pool.usage.columns.endpoint') },
    { key: 'group_name', label: t('admin.pool.extra.group') },
    { key: 'stream_type', label: t('admin.pool.extra.type') },
    { key: 'tokens', label: t('admin.pool.usage.columns.tokens'), class: 'text-right' },
    { key: 'cost', label: t('admin.pool.usage.columns.cost'), class: 'text-right' },
    { key: 'duration', label: t('admin.pool.usage.columns.duration'), class: 'text-right' },
    { key: 'created_at', label: t('admin.pool.usage.columns.time') },
    { key: 'user_agent', label: t('admin.pool.extra.userAgent') },
    { key: 'ip_address', label: t('admin.pool.extra.ipAddress') }
  )
  return base
})

/** 账号列优先用写入时固化的快照，改名或删除后历史仍可读。 */
function accountLabel(row: AdminUsageLog & UsageLog): string {
  const snapshot = row.account_name_snapshot
  if (snapshot) return snapshot
  if (row.account?.name) return row.account.name
  return row.account_id ? t('admin.pool.usage.accountDeleted') : '—'
}

/** 费用：管理员看账号侧统计成本，用户看自己实付。 */
function rowCost(row: AdminUsageLog & UsageLog): number {
  if (isAdmin.value) return row.account_stats_cost ?? row.total_cost ?? 0
  return row.actual_cost ?? row.total_cost ?? 0
}

function toRFC3339(dateStr: string, endOfDay: boolean): string | undefined {
  if (!dateStr) return undefined
  const local = new Date(`${dateStr}T${endOfDay ? '23:59:59.999' : '00:00:00.000'}`)
  if (Number.isNaN(local.getTime())) return undefined
  return local.toISOString()
}

function parseId(raw: string): number | undefined {
  const trimmed = raw.trim()
  if (!trimmed) return undefined
  const value = Number(trimmed)
  return Number.isFinite(value) && value > 0 ? Math.trunc(value) : undefined
}

let usageAbort: AbortController | null = null

async function fetchUsage() {
  usageAbort?.abort()
  usageAbort = new AbortController()
  usageLoading.value = true
  try {
    const params: PoolUsageQuery = {
      page: usagePage.value,
      page_size: usagePageSize.value,
      start_time: toRFC3339(usageStartDate.value, false),
      end_time: toRFC3339(usageEndDate.value, true)
    }
    const accountId = parseId(usageFilters.account_id)
    if (accountId) params.account_id = accountId
    // user_id 只有管理员能筛；用户端的 user_id 由服务端强制钉上当前登录用户。
    if (isAdmin.value) {
      const userId = parseId(usageFilters.user_id)
      if (userId) params.user_id = userId
    }
    const apiKeyId = parseId(usageFilters.api_key_id)
    if (apiKeyId) params.api_key_id = apiKeyId
    if (usageFilters.model) params.model = usageFilters.model
    if (usageFilters.request_id) params.request_id = usageFilters.request_id

    const result = await listPoolUsage(params, scope.value, { signal: usageAbort.signal })
    usageRows.value = result.items ?? []
    usageTotal.value = result.total ?? 0
    usageLoaded.value = true
  } catch (error) {
    if ((error as { name?: string })?.name === 'CanceledError') return
    usageRows.value = []
    usageTotal.value = 0
    appStore.showError(t('admin.pool.usage.loadFailed'))
  } finally {
    usageLoading.value = false
  }
}

function searchUsage() {
  usagePage.value = 1
  fetchUsage()
}

function resetUsageFilters() {
  usageFilters.account_id = ''
  usageFilters.user_id = ''
  usageFilters.api_key_id = ''
  usageFilters.model = ''
  usageFilters.request_id = ''
  const now = new Date()
  usageStartDate.value = formatLocalDate(new Date(now.getTime() - 24 * 60 * 60 * 1000))
  usageEndDate.value = formatLocalDate(now)
  searchUsage()
}

function onDateRangeChange(range: { startDate: string; endDate: string }) {
  usageStartDate.value = range.startDate
  usageEndDate.value = range.endDate
  searchUsage()
}

function onUsagePageChange(p: number) {
  usagePage.value = p
  fetchUsage()
}

function onUsagePageSizeChange(ps: number) {
  usagePageSize.value = ps
  usagePage.value = 1
  setPersistedPageSize(ps)
  fetchUsage()
}

// ==================== 账号请求流水 ====================

const requestsVisible = ref(false)
const requestsLoading = ref(false)
const requestRows = ref<(AdminUsageLog & UsageLog)[]>([])
const requestsTotal = ref(0)
const requestPage = ref(1)
const requestPageSize = ref(20)
const activeAccount = ref<PoolAccount | null>(null)

async function loadRequests() {
  if (!activeAccount.value) return
  requestsLoading.value = true
  try {
    const result = await listPoolAccountRequests(
      activeAccount.value.id,
      { page: requestPage.value, page_size: requestPageSize.value },
      scope.value
    )
    requestRows.value = result.items ?? []
    requestsTotal.value = result.total ?? 0
  } catch {
    requestRows.value = []
    requestsTotal.value = 0
    appStore.showError(t('admin.pool.dialog.loadFailed'))
  } finally {
    requestsLoading.value = false
  }
}

function openRequests(row: PoolAccount) {
  activeAccount.value = row
  requestPage.value = 1
  requestRows.value = []
  requestsTotal.value = 0
  requestsVisible.value = true
  loadRequests()
}

function closeRequests() {
  requestsVisible.value = false
  activeAccount.value = null
  requestRows.value = []
}

function onRequestPageChange(p: number) {
  requestPage.value = p
  loadRequests()
}

async function loadGroupOptions() {
  if (!isAdmin.value) return
  try {
    const groups = await listAdminGroups()
    groupOptions.value = [
      { value: '', label: t('admin.pool.filters.allGroups') },
      ...(groups ?? []).map(g => ({ value: String(g.id), label: g.name }))
    ]
  } catch {
    // 分组接口不可用时只是少一个筛选器，不影响号池主功能。
    groupOptions.value = [{ value: '', label: t('admin.pool.filters.allGroups') }]
  }
}

onMounted(() => {
  refreshAccounts()
  void loadGroupOptions()
  if (activeTab.value === 'usage') fetchUsage()
})
</script>
