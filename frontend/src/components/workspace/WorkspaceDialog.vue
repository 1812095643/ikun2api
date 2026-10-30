<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
const props = defineProps<{ open: boolean; title: string; wide?: boolean }>()
const emit = defineEmits<{ 'update:open': [open: boolean] }>()
const dialog = ref<HTMLDialogElement | null>(null)
watch(
  () => props.open,
  async (open) => {
    await nextTick()
    if (open && !dialog.value?.open) dialog.value?.showModal()
    else if (!open) dialog.value?.close()
  },
  { immediate: true }
)
onBeforeUnmount(() => dialog.value?.close())
</script>

<template>
  <dialog
    ref="dialog"
    class="workspace-dialog"
    :class="{ 'workspace-dialog-wide': wide }"
    :aria-label="title"
    @cancel.prevent="emit('update:open', false)"
    @click="
      (event) => {
        if (event.target === dialog) emit('update:open', false)
      }
    "
  >
    <div class="workspace-dialog-body">
      <header class="workspace-dialog-header">
        <h2>{{ title }}</h2>
        <button
          class="ws-icon-btn"
          type="button"
          aria-label="关闭"
          @click="emit('update:open', false)"
        >
          <Icon
            name="x"
            size="sm"
          />
        </button>
      </header>
      <slot />
      <footer
        v-if="$slots.footer"
        class="workspace-dialog-footer"
      >
        <slot name="footer" />
      </footer>
    </div>
  </dialog>
</template>
