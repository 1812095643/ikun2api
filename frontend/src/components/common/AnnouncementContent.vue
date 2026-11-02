<script setup lang="ts">
import { computed } from 'vue'
import { renderAnnouncement } from '@/utils/announcementContent'
import '@/styles/announcement-markdown.css'
const props = defineProps<{ content: string }>()
const html = computed(() => renderAnnouncement(props.content))
</script>
<template><article class="markdown-body announcement-content" v-html="html"></article></template>
