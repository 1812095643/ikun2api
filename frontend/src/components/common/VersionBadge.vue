<script setup lang="ts">
import { computed, shallowRef, ref, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { getRollbackVersions, type RollbackVersionInfo } from '@/api/admin/system'
import { useClipboard } from '@/composables/useClipboard'
import { useSystemUpdate } from '@/composables/useSystemUpdate'
import Icon from '@/components/icons/Icon.vue'

const GITHUB_REPO = '1812095643/ikun2api'
const DOCKER_IMAGE = 'ghcr.io/1812095643/ikun2api'
const { t } = useI18n()
const props = defineProps<{ version?: string }>()
const authStore = useAuthStore()
const isAdmin = computed(() => authStore.isAdmin)
const system = useSystemUpdate({ autoRestart: false })
const { info, phase, error, busy, needRestart } = system
const dropdownOpen = shallowRef(false)
const dropdownRef = ref<HTMLElement | null>(null)
const rootRef = ref<HTMLElement | null>(null)
const loading = computed(() => phase.value === 'checking')
const currentVersion = computed(() => info.value?.current_version || props.version || '')
const latestVersion = computed(() => info.value?.latest_version || '')
const hasUpdate = computed(() => info.value?.has_update ?? false)
const releaseInfo = computed(() => info.value?.release_info)
const isReleaseBuild = computed(() => info.value?.supports_self_update === true)
const successKind = shallowRef<'update' | 'rollback'>('update')
const updating = computed(() => busy.value && !loading.value && successKind.value === 'update')
const restarting = computed(() => phase.value === 'restarting')
const restartCountdown = shallowRef(0)
const updateSuccess = computed(() => needRestart.value)
const updateError = computed(() => successKind.value === 'update' ? error.value : '')
const rollbackError = computed(() => successKind.value === 'rollback' ? error.value : '')
const rollbackPanelOpen = shallowRef(false)
const rollbackVersions = ref<RollbackVersionInfo[]>([])
const rollbackVersionsLoading = shallowRef(false)
const rollbackVersionsError = shallowRef('')
const selectedRollbackVersion = shallowRef('')
const rollingBack = computed(() => busy.value && successKind.value === 'rollback')
const { copied, copyToClipboard } = useClipboard()
const manualTab = shallowRef<'script' | 'docker'>('script')
const manualTabs = computed(() => [
  { key: 'script' as const, label: t('version.deployScript') },
  { key: 'docker' as const, label: t('version.deployDocker') },
])
const scriptRollbackCommand = computed(() => {
  if (!selectedRollbackVersion.value) return ''
  return `curl -fsSL https://raw.githubusercontent.com/${GITHUB_REPO}/main/deploy/switch-from-sub2api.py -o /tmp/ikun2api-switch.py && sudo python3 /tmp/ikun2api-switch.py --service ikun2api --version v${selectedRollbackVersion.value}`
})
const dockerRollbackCommand = computed(() => !selectedRollbackVersion.value ? '' : [
  `# ${t('version.dockerEditCompose')}`, `image: ${DOCKER_IMAGE}:${selectedRollbackVersion.value}`, '',
  `# ${t('version.dockerRecreate')}`, 'docker compose up -d',
].join('\n'))
const activeManualCommand = computed(() => manualTab.value === 'docker' ? dockerRollbackCommand.value : scriptRollbackCommand.value)

function toggleDropdown() { dropdownOpen.value = !dropdownOpen.value }
function closeDropdown() { if (!busy.value) dropdownOpen.value = false }
async function refreshVersion(force = true) {
  if (!isAdmin.value || busy.value) return
  selectedRollbackVersion.value = ''; rollbackVersionsError.value = ''; successKind.value = 'update'
  await system.check(force)
}
async function handleUpdate() {
  if (!isAdmin.value || busy.value || !isReleaseBuild.value) return
  successKind.value = 'update'
  await system.update()
}
async function toggleRollbackPanel() {
  if (!isAdmin.value || busy.value) return
  rollbackPanelOpen.value = !rollbackPanelOpen.value
  if (rollbackPanelOpen.value && isReleaseBuild.value && !rollbackVersions.value.length) await loadRollbackVersions()
}
async function loadRollbackVersions() {
  if (!isAdmin.value || rollbackVersionsLoading.value) return
  rollbackVersionsLoading.value = true; rollbackVersionsError.value = ''
  try { rollbackVersions.value = (await getRollbackVersions()).versions || [] }
  catch (e) {
    const err = e as { response?: { data?: { message?: string } }; message?: string }
    rollbackVersionsError.value = err.response?.data?.message || err.message || t('version.loadVersionsFailed')
  } finally { rollbackVersionsLoading.value = false }
}
function selectRollbackVersion(version: string) {
  if (busy.value) return
  error.value = ''
  selectedRollbackVersion.value = selectedRollbackVersion.value === version ? '' : version
}
function formatPublishedAt(value: string): string {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toLocaleDateString()
}
async function handleRollback() {
  if (!isAdmin.value || busy.value || !selectedRollbackVersion.value || !isReleaseBuild.value) return
  successKind.value = 'rollback'
  await system.rollback(selectedRollbackVersion.value)
  if (!error.value) rollbackPanelOpen.value = false
}
async function handleRestart() {
  if (!isAdmin.value || busy.value) return
  await system.restart()
}
function handleClickOutside(event: MouseEvent) {
  if (rootRef.value && !rootRef.value.contains(event.target as Node)) closeDropdown()
}
function handleEscape(event: KeyboardEvent) { if (event.key === 'Escape') closeDropdown() }
onMounted(() => {
  if (isAdmin.value) void system.check(false)
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleEscape)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleEscape)
})
</script>

<template>
  <div ref="rootRef" class="relative">
    <!-- Admin: Full version badge with dropdown -->
    <template v-if="isAdmin">
      <button
        type="button"
        @click="toggleDropdown"
        class="flex items-center gap-1.5 rounded-lg px-2 py-1 text-xs transition-colors"
        :class="[
          hasUpdate
            ? 'bg-amber-100 text-amber-700 hover:bg-amber-200 dark:bg-amber-900/30 dark:text-amber-400 dark:hover:bg-amber-900/50'
            : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-800 dark:text-dark-400 dark:hover:bg-dark-700'
        ]"
        :title="hasUpdate ? t('version.updateAvailable') : t('version.upToDate')"
        :aria-expanded="dropdownOpen"
        aria-haspopup="true"
      >
        <span v-if="currentVersion" class="font-medium">v{{ currentVersion }}</span>
        <span
          v-else
          class="h-3 w-12 animate-pulse rounded bg-gray-200 font-medium dark:bg-dark-600"
        ></span>
        <!-- Update indicator -->
        <span v-if="hasUpdate" class="relative flex h-2 w-2">
          <span
            class="absolute inline-flex h-full w-full animate-ping rounded-full bg-amber-400 opacity-75"
          ></span>
          <span class="relative inline-flex h-2 w-2 rounded-full bg-amber-500"></span>
        </span>
      </button>

      <!-- Dropdown -->
      <transition name="dropdown">
        <div
          v-if="dropdownOpen"
          ref="dropdownRef"
          class="absolute left-0 z-50 mt-2 overflow-hidden whitespace-normal rounded-xl border border-gray-200 bg-white shadow-lg transition-all duration-200 dark:border-dark-700 dark:bg-dark-800"
          :class="rollbackPanelOpen && isReleaseBuild ? 'w-80' : 'w-64'"
        >
          <!-- Header with refresh button -->
          <div
            class="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-dark-700"
          >
            <span class="text-sm font-medium text-gray-700 dark:text-dark-300">{{
              t('version.currentVersion')
            }}</span>
            <button
              @click="refreshVersion(true)"
              class="rounded-lg p-1.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-dark-700 dark:hover:text-dark-200"
              :disabled="loading"
              :title="t('version.refresh')"
            >
              <Icon
                name="refresh"
                size="sm"
                :stroke-width="2"
                :class="{ 'animate-spin': loading }"
              />
            </button>
          </div>

          <div class="p-4">
            <!-- Loading state -->
            <div v-if="loading" class="flex items-center justify-center py-6">
              <svg class="h-6 w-6 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
                <circle
                  class="opacity-25"
                  cx="12"
                  cy="12"
                  r="10"
                  stroke="currentColor"
                  stroke-width="4"
                ></circle>
                <path
                  class="opacity-75"
                  fill="currentColor"
                  d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                ></path>
              </svg>
            </div>

            <!-- Content -->
            <template v-else>
              <p v-if="info?.warning" role="status" class="mb-3 break-words text-xs text-amber-600 dark:text-amber-400">{{ info.warning }}</p>
              <!-- Version display - centered and prominent -->
              <div class="mb-4 text-center">
                <div class="inline-flex items-center gap-2">
                  <span
                    v-if="currentVersion"
                    class="text-2xl font-bold text-gray-900 dark:text-white"
                    >v{{ currentVersion }}</span
                  >
                  <span v-else class="text-2xl font-bold text-gray-400 dark:text-dark-500">--</span>
                  <!-- Show check mark when up to date -->
                  <span
                    v-if="!hasUpdate"
                    class="flex h-5 w-5 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/30"
                  >
                    <svg
                      class="h-3 w-3 text-green-600 dark:text-green-400"
                      fill="currentColor"
                      viewBox="0 0 20 20"
                    >
                      <path
                        fill-rule="evenodd"
                        d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
                        clip-rule="evenodd"
                      />
                    </svg>
                  </span>
                </div>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                  {{
                    hasUpdate
                      ? t('version.latestVersion') + ': v' + latestVersion
                      : t('version.upToDate')
                  }}
                </p>
              </div>

              <!-- Priority 1: Update error (must check before hasUpdate) -->
              <div v-if="updateError" class="space-y-2">
                <div
                  class="flex items-center gap-3 rounded-lg border border-red-200 bg-red-50 p-3 dark:border-red-800/50 dark:bg-red-900/20"
                >
                  <div
                    class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-red-100 dark:bg-red-900/50"
                  >
                    <Icon
                      name="x"
                      size="sm"
                      :stroke-width="2"
                      class="text-red-600 dark:text-red-400"
                    />
                  </div>
                  <div class="min-w-0 flex-1">
                    <p class="text-sm font-medium text-red-700 dark:text-red-300">
                      {{ t('version.updateFailed') }}
                    </p>
                    <p class="truncate text-xs text-red-600/70 dark:text-red-400/70">
                      {{ updateError }}
                    </p>
                  </div>
                </div>

                <!-- Retry button -->
                <button
                  @click="handleUpdate"
                  :disabled="updating"
                  class="flex w-full items-center justify-center gap-2 rounded-lg bg-red-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-red-600 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {{ t('version.retry') }}
                </button>
              </div>

              <!-- Priority 2: Update success - need restart -->
              <div v-else-if="updateSuccess && needRestart" class="space-y-2">
                <div
                  class="flex items-center gap-3 rounded-lg border border-green-200 bg-green-50 p-3 dark:border-green-800/50 dark:bg-green-900/20"
                >
                  <div
                    class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/50"
                  >
                    <svg
                      class="h-4 w-4 text-green-600 dark:text-green-400"
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                      stroke-width="2"
                    >
                      <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
                    </svg>
                  </div>
                  <div class="min-w-0 flex-1">
                    <p class="text-sm font-medium text-green-700 dark:text-green-300">
                      {{
                        successKind === 'rollback'
                          ? t('version.rollbackComplete')
                          : t('version.updateComplete')
                      }}
                    </p>
                    <p class="text-xs text-green-600/70 dark:text-green-400/70">
                      {{ t('version.restartRequired') }}
                    </p>
                  </div>
                </div>

                <!-- Restart button with countdown -->
                <button
                  @click="handleRestart"
                  :disabled="restarting"
                  class="flex w-full items-center justify-center gap-2 rounded-lg bg-green-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-green-600 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <svg
                    v-if="restarting"
                    class="h-4 w-4 animate-spin"
                    fill="none"
                    viewBox="0 0 24 24"
                  >
                    <circle
                      class="opacity-25"
                      cx="12"
                      cy="12"
                      r="10"
                      stroke="currentColor"
                      stroke-width="4"
                    ></circle>
                    <path
                      class="opacity-75"
                      fill="currentColor"
                      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                    ></path>
                  </svg>
                  <svg
                    v-else
                    class="h-4 w-4"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
                    />
                  </svg>
                  <template v-if="restarting">
                    <span>{{ t('version.restarting') }}</span>
                    <span v-if="restartCountdown > 0" class="tabular-nums"
                      >({{ restartCountdown }}s)</span
                    >
                  </template>
                  <span v-else>{{ t('version.restartNow') }}</span>
                </button>
              </div>

              <!-- Priority 3: Update available for source build - show git pull hint -->
              <div v-else-if="hasUpdate && !isReleaseBuild" class="space-y-2">
                <a
                  v-if="releaseInfo?.html_url && releaseInfo.html_url !== '#'"
                  :href="releaseInfo.html_url"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="group flex items-center gap-3 rounded-lg border border-amber-200 bg-amber-50 p-3 transition-colors hover:bg-amber-100 dark:border-amber-800/50 dark:bg-amber-900/20 dark:hover:bg-amber-900/30"
                >
                  <div
                    class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-amber-100 dark:bg-amber-900/50"
                  >
                    <Icon
                      name="download"
                      size="sm"
                      :stroke-width="2"
                      class="text-amber-600 dark:text-amber-400"
                    />
                  </div>
                  <div class="min-w-0 flex-1">
                    <p class="text-sm font-medium text-amber-700 dark:text-amber-300">
                      {{ t('version.updateAvailable') }}
                    </p>
                    <p class="text-xs text-amber-600/70 dark:text-amber-400/70">
                      v{{ latestVersion }}
                    </p>
                  </div>
                  <svg
                    class="h-4 w-4 text-amber-500 transition-transform group-hover:translate-x-0.5 dark:text-amber-400"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
                  </svg>
                </a>
                <!-- Source build hint -->
                <div
                  class="flex items-center gap-2 rounded-lg border border-blue-200 bg-blue-50 p-2 dark:border-blue-800/50 dark:bg-blue-900/20"
                >
                  <svg
                    class="h-3.5 w-3.5 flex-shrink-0 text-blue-500 dark:text-blue-400"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                    />
                  </svg>
                  <p class="text-xs text-blue-600 dark:text-blue-400">
                    {{ t('version.sourceModeHint') }}
                  </p>
                </div>
              </div>

              <!-- Priority 4: Update available for release build - show update button -->
              <div v-else-if="hasUpdate && isReleaseBuild" class="space-y-2">
                <!-- Update info card -->
                <div
                  class="flex items-center gap-3 rounded-lg border border-amber-200 bg-amber-50 p-3 dark:border-amber-800/50 dark:bg-amber-900/20"
                >
                <div
                  class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-amber-100 dark:bg-amber-900/50"
                >
                  <Icon
                    name="download"
                    size="sm"
                    :stroke-width="2"
                    class="text-amber-600 dark:text-amber-400"
                  />
                </div>
                  <div class="min-w-0 flex-1">
                    <p class="text-sm font-medium text-amber-700 dark:text-amber-300">
                      {{ t('version.updateAvailable') }}
                    </p>
                    <p class="text-xs text-amber-600/70 dark:text-amber-400/70">
                      v{{ latestVersion }}
                    </p>
                  </div>
                </div>

                <!-- Update button -->
                <button
                  @click="handleUpdate"
                  :disabled="updating"
                  class="flex w-full items-center justify-center gap-2 rounded-lg bg-primary-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-primary-600 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <svg v-if="updating" class="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle
                      class="opacity-25"
                      cx="12"
                      cy="12"
                      r="10"
                      stroke="currentColor"
                      stroke-width="4"
                    ></circle>
                    <path
                      class="opacity-75"
                      fill="currentColor"
                      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                    ></path>
                  </svg>
                  <Icon v-else name="download" size="sm" :stroke-width="2" />
                  {{ updating ? (phase === 'interrupting' ? t('version.interrupting') : t('version.updating')) : t('version.updateNow') }}
                </button>
                <p class="text-[11px] leading-4 text-gray-500 dark:text-dark-400">{{ t('version.updateHint') }}</p>

                <!-- View release link -->
                <a
                  v-if="releaseInfo?.html_url && releaseInfo.html_url !== '#'"
                  :href="releaseInfo.html_url"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="flex items-center justify-center gap-1 text-xs text-gray-500 transition-colors hover:text-gray-700 dark:text-dark-400 dark:hover:text-dark-200"
                >
                  {{ t('version.viewChangelog') }}
                  <Icon name="externalLink" size="xs" :stroke-width="2" />
                </a>
              </div>

              <!-- Priority 5: Up to date - version rollback -->
              <div v-else class="space-y-2">
                <!-- Version rollback entry -->
                <div class="border-t border-gray-100 pt-2 dark:border-dark-700">
                  <button
                    @click="toggleRollbackPanel"
                    class="group flex w-full items-center justify-between rounded-lg px-2 py-1.5 text-xs text-gray-400 transition-colors hover:bg-gray-50 hover:text-gray-600 dark:text-dark-500 dark:hover:bg-dark-700/50 dark:hover:text-dark-300"
                  >
                    <span class="flex items-center gap-1.5">
                      <Icon name="clock" size="xs" :stroke-width="2" />
                      {{ t('version.rollback') }}
                    </span>
                    <Icon
                      name="chevronDown"
                      size="xs"
                      :stroke-width="2"
                      class="transition-transform duration-200"
                      :class="{ 'rotate-180': rollbackPanelOpen }"
                    />
                  </button>

                  <transition name="rollback">
                    <div v-if="rollbackPanelOpen" class="mt-2 space-y-2">
                      <!-- Source build: online rollback unavailable, use git instead -->
                      <div
                        v-if="!isReleaseBuild"
                        class="flex items-center gap-2 rounded-lg border border-blue-200 bg-blue-50 p-2 dark:border-blue-800/50 dark:bg-blue-900/20"
                      >
                        <svg
                          class="h-3.5 w-3.5 flex-shrink-0 text-blue-500 dark:text-blue-400"
                          fill="none"
                          viewBox="0 0 24 24"
                          stroke="currentColor"
                          stroke-width="2"
                        >
                          <path
                            stroke-linecap="round"
                            stroke-linejoin="round"
                            d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                          />
                        </svg>
                        <p class="min-w-0 flex-1 text-xs leading-4 text-blue-600 dark:text-blue-400">
                          {{ t('version.rollbackSourceHint') }}
                        </p>
                      </div>

                      <!-- Loading versions -->
                      <div
                        v-else-if="rollbackVersionsLoading"
                        class="flex items-center justify-center py-4"
                      >
                        <svg
                          class="h-5 w-5 animate-spin text-primary-500"
                          fill="none"
                          viewBox="0 0 24 24"
                        >
                          <circle
                            class="opacity-25"
                            cx="12"
                            cy="12"
                            r="10"
                            stroke="currentColor"
                            stroke-width="4"
                          ></circle>
                          <path
                            class="opacity-75"
                            fill="currentColor"
                            d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                          ></path>
                        </svg>
                      </div>

                      <!-- Load error + retry -->
                      <div v-else-if="rollbackVersionsError" class="space-y-2">
                        <p
                          class="rounded-lg border border-red-200 bg-red-50 p-2.5 text-xs text-red-600 dark:border-red-800/50 dark:bg-red-900/20 dark:text-red-400"
                        >
                          {{ rollbackVersionsError }}
                        </p>
                        <button
                          @click="loadRollbackVersions"
                          class="w-full rounded-lg border border-gray-200 py-1.5 text-xs text-gray-500 transition-colors hover:bg-gray-50 hover:text-gray-700 dark:border-dark-700 dark:text-dark-400 dark:hover:bg-dark-700/50 dark:hover:text-dark-200"
                        >
                          {{ t('version.retry') }}
                        </button>
                      </div>

                      <!-- No versions available -->
                      <p
                        v-else-if="rollbackVersions.length === 0"
                        class="py-3 text-center text-xs text-gray-400 dark:text-dark-500"
                      >
                        {{ t('version.noRollbackVersions') }}
                      </p>

                      <!-- Version list -->
                      <template v-else>
                        <p class="px-0.5 text-[11px] text-gray-400 dark:text-dark-500">
                          {{ t('version.rollbackSelectVersion') }}
                        </p>

                        <button
                          v-for="item in rollbackVersions"
                          :key="item.version"
                          @click="selectRollbackVersion(item.version)"
                          :disabled="rollingBack"
                          class="flex w-full items-center justify-between rounded-lg border px-3 py-2 text-left transition-all disabled:cursor-not-allowed disabled:opacity-60"
                          :class="
                            selectedRollbackVersion === item.version
                              ? 'border-amber-300 bg-amber-50 shadow-sm dark:border-amber-700 dark:bg-amber-900/20'
                              : 'border-gray-200 hover:border-gray-300 hover:bg-gray-50 dark:border-dark-700 dark:hover:border-dark-600 dark:hover:bg-dark-700/40'
                          "
                        >
                          <span class="flex items-center gap-2">
                            <span
                              class="flex h-3.5 w-3.5 items-center justify-center rounded-full border transition-colors"
                              :class="
                                selectedRollbackVersion === item.version
                                  ? 'border-amber-500'
                                  : 'border-gray-300 dark:border-dark-500'
                              "
                            >
                              <span
                                v-if="selectedRollbackVersion === item.version"
                                class="h-1.5 w-1.5 rounded-full bg-amber-500"
                              ></span>
                            </span>
                            <span
                              class="text-sm font-semibold"
                              :class="
                                selectedRollbackVersion === item.version
                                  ? 'text-amber-700 dark:text-amber-300'
                                  : 'text-gray-700 dark:text-dark-200'
                              "
                              >v{{ item.version }}</span
                            >
                          </span>
                          <span class="text-[11px] tabular-nums text-gray-400 dark:text-dark-500">
                            {{ formatPublishedAt(item.published_at) }}
                          </span>
                        </button>

                        <!-- Selected version: manual command (per deploy method) + confirm -->
                        <transition name="rollback">
                          <div v-if="selectedRollbackVersion" class="space-y-2">
                            <p class="px-0.5 text-[11px] text-gray-400 dark:text-dark-500">
                              {{ t('version.manualRollbackCommand') }}
                            </p>

                            <!-- Terminal-style block with deploy-method tabs -->
                            <div
                              class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600"
                            >
                              <div
                                class="flex items-center justify-between border-b border-gray-200 bg-gray-100 px-2 py-1.5 dark:border-dark-600 dark:bg-dark-700"
                              >
                                <div
                                  class="flex items-center gap-0.5 rounded-md bg-gray-200/70 p-0.5 dark:bg-dark-600/70"
                                >
                                  <button
                                    v-for="tab in manualTabs"
                                    :key="tab.key"
                                    @click="manualTab = tab.key"
                                    class="rounded px-2 py-0.5 text-[11px] font-medium transition-colors"
                                    :class="
                                      manualTab === tab.key
                                        ? 'bg-white text-gray-700 shadow-sm dark:bg-dark-800 dark:text-dark-100'
                                        : 'text-gray-400 hover:text-gray-600 dark:text-dark-400 dark:hover:text-dark-200'
                                    "
                                  >
                                    {{ tab.label }}
                                  </button>
                                </div>
                                <button
                                  @click="copyToClipboard(activeManualCommand)"
                                  class="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-gray-400 transition-colors hover:bg-gray-200 hover:text-gray-600 dark:text-dark-400 dark:hover:bg-dark-600 dark:hover:text-dark-200"
                                >
                                  <Icon
                                    :name="copied ? 'check' : 'copy'"
                                    size="xs"
                                    :stroke-width="2"
                                    :class="copied ? 'text-green-500' : ''"
                                  />
                                  {{ copied ? t('version.copied') : t('version.copyCommand') }}
                                </button>
                              </div>
                              <code
                                class="block select-all whitespace-pre-wrap break-all bg-gray-50 p-2.5 font-mono text-[10px] leading-relaxed text-gray-600 dark:bg-dark-900 dark:text-dark-300"
                                >{{ activeManualCommand }}</code
                              >
                            </div>

                            <p
                              class="flex items-start gap-1.5 px-0.5 text-[11px] leading-4 text-amber-600 dark:text-amber-400"
                            >
                              <Icon
                                name="exclamationTriangle"
                                size="xs"
                                :stroke-width="2"
                                class="mt-px flex-shrink-0"
                              />
                              {{ t('version.rollbackWarning') }}
                            </p>

                            <p
                              v-if="rollbackError"
                              class="rounded-lg border border-red-200 bg-red-50 p-2 text-xs text-red-600 dark:border-red-800/50 dark:bg-red-900/20 dark:text-red-400"
                            >
                              {{ rollbackError }}
                            </p>

                            <button
                              @click="handleRollback"
                              :disabled="rollingBack"
                              class="flex w-full items-center justify-center gap-2 rounded-lg bg-amber-500 px-4 py-2 text-sm font-medium text-white shadow-sm transition-colors hover:bg-amber-600 disabled:cursor-not-allowed disabled:opacity-50"
                            >
                              <svg
                                v-if="rollingBack"
                                class="h-4 w-4 animate-spin"
                                fill="none"
                                viewBox="0 0 24 24"
                              >
                                <circle
                                  class="opacity-25"
                                  cx="12"
                                  cy="12"
                                  r="10"
                                  stroke="currentColor"
                                  stroke-width="4"
                                ></circle>
                                <path
                                  class="opacity-75"
                                  fill="currentColor"
                                  d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                                ></path>
                              </svg>
                              <Icon v-else name="clock" size="sm" :stroke-width="2" />
                              <span>{{
                                rollingBack
                                  ? t('version.rollingBack')
                                  : t('version.rollbackConfirm', {
                                      version: 'v' + selectedRollbackVersion
                                    })
                              }}</span>
                            </button>
                          </div>
                        </transition>
                      </template>
                    </div>
                  </transition>
                </div>
              </div>
            </template>
          </div>
        </div>
      </transition>
    </template>

    <!-- Non-admin: Simple static version text -->
    <span v-else-if="version" class="text-xs text-gray-500 dark:text-dark-400">
      v{{ version }}
    </span>
  </div>
</template>

<style scoped>
.dropdown-enter-active,
.dropdown-leave-active {
  transition: all 0.2s ease;
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: scale(0.95) translateY(-4px);
}

.rollback-enter-active,
.rollback-leave-active {
  transition: all 0.2s ease;
}

.rollback-enter-from,
.rollback-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

.line-clamp-3 {
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
</style>
