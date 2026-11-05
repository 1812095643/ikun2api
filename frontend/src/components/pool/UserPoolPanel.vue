<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { createPoolShareLink, revokePoolShareLink, type UserPoolAccount } from '@/api/pool'
import { useUserPoolData, type UserPoolTab } from '@/composables/useUserPoolData'
import { usePoolRefresh } from '@/composables/usePoolRefresh'
import { useClipboard } from '@/composables/useClipboard'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import UsageProgressBar from '@/components/account/UsageProgressBar.vue'
import CapacityBadge from '@/components/account/CapacityBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PoolNetworkError from './PoolNetworkError.vue'
import UserPoolUsageTable from './UserPoolUsageTable.vue'
const props=defineProps<{ shared?: boolean; token?: string; initialTab?: UserPoolTab }>()
const { t }=useI18n()
const { activeTab, invalid, loading, failed, updatedAt, rows, usageRows, total, currentTotal, currentPage, refreshCurrent } = useUserPoolData({
  shared: () => props.shared, token: () => props.token, initialTab: () => props.initialTab
})
const shareVisible=ref(false),shareBusy=ref(false),shareError=ref(''),shareURL=ref(''),expires=ref(''),copied=ref(false)
const columns=computed(()=>[{key:'name',label:t('admin.pool.columns.account')},{key:'platform',label:t('admin.pool.filters.platform')},{key:'status',label:t('admin.pool.columns.status')},{key:'capacity',label:t('admin.pool.columns.concurrency')},{key:'usage_window',label:t('admin.pool.extra.usageWindow')}])
const {copyToClipboard}=useClipboard()
const {online}=usePoolRefresh(refreshCurrent,()=>loading.value||invalid.value||shareVisible.value)
function capacityColor(row:UserPoolAccount){const current=row.capacity?.in_use??0,max=row.capacity?.concurrency??0;return current>=max&&max>0?'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400':current>0?'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400':'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400'}
async function generate(){
  if(shareBusy.value)return
  shareBusy.value=true;shareError.value='';copied.value=false
  try{const result=await createPoolShareLink();shareURL.value=`${location.origin}/share/pool#${new URLSearchParams({token:result.token,tab:activeTab.value})}`;expires.value=result.expires_at}
  catch{shareError.value=t('admin.pool.networkError')}
  finally{shareBusy.value=false}
}
async function revoke(){
  if(shareBusy.value)return
  shareBusy.value=true;shareError.value=''
  try{await revokePoolShareLink();shareURL.value='';expires.value='';copied.value=false;shareVisible.value=false}
  catch{shareError.value=t('admin.pool.networkError')}
  finally{shareBusy.value=false}
}
async function copy(){copied.value=await copyToClipboard(shareURL.value)}
function label(status:string){return t(`admin.pool.status.${['active','error','disabled'].includes(status)?status:'unschedulable'}`)}
</script>
<template>
  <section class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex flex-wrap items-center gap-3"><div class="flex rounded-xl bg-gray-100 p-1 dark:bg-dark-800" role="tablist"><button v-for="tab in (['accounts','usage'] as const)" :key="tab" role="tab" :aria-selected="activeTab===tab" :disabled="invalid" class="rounded-lg px-4 py-2 text-sm" :class="activeTab===tab?'bg-white text-primary-600 shadow-sm dark:bg-dark-700 dark:text-primary-400':'text-gray-500 dark:text-dark-400'" @click="activeTab=tab">{{ t(tab==='accounts'?'admin.pool.navAccounts':'admin.pool.navUsage') }}</button></div><span v-if="activeTab==='accounts'" class="text-sm text-gray-500 dark:text-dark-400">{{ t('admin.pool.visibleAccounts',{count:total}) }}</span></div>
      <div class="flex gap-2"><button type="button" class="btn btn-secondary" :disabled="loading||invalid" @click="refreshCurrent"><Icon name="refresh" size="sm" :class="{'animate-spin':loading}" />{{ t('common.refresh') }}</button><button v-if="!shared" type="button" class="btn btn-primary" @click="shareVisible=true">{{ t('admin.pool.sharePool') }}</button></div>
    </div>
    <div v-if="invalid" role="alert" class="rounded-xl bg-gray-100 p-8 text-center text-gray-600 dark:bg-dark-800 dark:text-dark-300">{{ t('admin.pool.shareInvalid') }}</div>
    <PoolNetworkError v-else-if="failed||!online" :updated-at="updatedAt" :busy="loading" @retry="refreshCurrent" />
    <div v-if="!invalid" class="card overflow-hidden">
      <DataTable v-if="activeTab==='accounts'" :columns="columns" :data="rows" :loading="loading&&!rows.length">
        <template #cell-name="{value}"><span class="font-medium text-gray-900 dark:text-white">{{ value }}</span></template>
        <template #cell-platform="{value}"><span class="badge badge-gray">{{ value === 'anthropic' || value === 'claude' ? 'Claude' : value }}</span></template>
        <template #cell-status="{value}"><StatusBadge :status="value" :label="label(value)" /></template>
        <template #cell-capacity="{row}"><CapacityBadge :color-class="capacityColor(row)" :current="row.capacity?.in_use??0" :max="row.capacity?.concurrency??0" /></template>
        <template #cell-usage_window="{row}"><div class="space-y-1"><UsageProgressBar v-if="row.five_hour?.enabled" label="5h" :utilization="row.five_hour.utilization" color="indigo" /><span v-else class="text-xs text-gray-400">5h —</span><UsageProgressBar v-if="row.seven_day?.enabled" label="7d" :utilization="row.seven_day.utilization" color="emerald" /><span v-else class="text-xs text-gray-400">7d —</span></div></template>
        <template #empty><p class="py-12 text-center text-sm text-gray-500 dark:text-dark-400">{{ failed?t('admin.pool.networkError'):t('admin.pool.user.empty') }}</p></template>
      </DataTable>
      <UserPoolUsageTable v-else :rows="usageRows" :loading="loading" :failed="failed" />
    </div>
    <Pagination v-if="currentTotal>20&&!invalid" :total="currentTotal" v-model:page="currentPage" :page-size="20" :show-page-size-selector="false" />
  </section>
  <BaseDialog :show="shareVisible" :title="t('admin.pool.sharePool')" width="wide" @close="!shareBusy&&(shareVisible=false)">
    <div class="space-y-4">
      <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('admin.pool.shareHint') }}</p>
      <div v-if="shareURL"><label for="pool-share-url" class="input-label">{{ t('admin.pool.shareLink') }}</label><input id="pool-share-url" :value="shareURL" readonly class="input text-sm" @focus="($event.target as HTMLInputElement).select()" /><p class="mt-2 text-xs text-gray-500">{{ t('admin.pool.shareExpires',{time:new Date(expires).toLocaleString()}) }}</p></div>
      <p v-if="shareError" role="alert" class="text-sm text-red-600">{{ shareError }}</p>
    </div>
    <template #footer>
      <button type="button" class="btn btn-secondary" :disabled="shareBusy" @click="revoke">{{ t('admin.pool.revokeShare') }}</button>
      <button type="button" class="btn btn-secondary" :disabled="shareBusy" @click="generate">{{ t(shareURL?'admin.pool.regenerateShare':'admin.pool.generateShare') }}</button>
      <button v-if="shareURL" type="button" class="btn btn-primary" @click="copy">{{ t(copied?'admin.pool.linkCopied':'admin.pool.copyLink') }}</button>
    </template>
  </BaseDialog>
</template>
