<script setup lang="ts">
import { useOnboardingTour } from '@/composables/useOnboardingTour'

// 位于路由出口之外，切换页面时保留同一个引导实例。
useOnboardingTour({ autoStart: true })
</script>

<template><span hidden aria-hidden="true"></span></template>
