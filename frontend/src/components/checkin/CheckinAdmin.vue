<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import CheckinAdminSettings from './CheckinAdminSettings.vue'
import BlindboxAdminSettings from './BlindboxAdminSettings.vue'
import BlindboxAdminHistory from './BlindboxAdminHistory.vue'
const tabs = [{ key: 'checkin', label: '签到设置', component: CheckinAdminSettings }, { key: 'blindbox', label: '盲盒设置', component: BlindboxAdminSettings }, { key: 'history', label: '场次与账目', component: BlindboxAdminHistory }] as const
const active = shallowRef<(typeof tabs)[number]['key']>('checkin')
const current = computed(() => tabs.find(tab => tab.key === active.value)!.component)
</script>
<template>
  <section class="checkin-admin">
    <nav class="mb-5 flex gap-5 border-b border-gray-200 dark:border-dark-700" aria-label="签到与盲盒管理">
      <button v-for="tab in tabs" :key="tab.key" type="button" class="border-b-2 px-1 pb-3 text-sm transition-colors" :class="active === tab.key ? 'border-blue-500 font-medium text-blue-600 dark:text-blue-400' : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'" :aria-pressed="active === tab.key" @click="active = tab.key">{{ tab.label }}</button>
    </nav>
    <KeepAlive><component :is="current" /></KeepAlive>
  </section>
</template>
<style scoped>
.checkin-admin { max-width: 1080px; }
</style>
