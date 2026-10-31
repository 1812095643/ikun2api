<script setup lang="ts">
import { onMounted, onUnmounted, shallowRef, watch } from 'vue'
import background from '@/assets/landing/prism-gate.webp'
import far from '@/assets/landing/prism-gate-far.webp'
import type { PrismScene } from './prismScene'

const props = defineProps<{ progress: number; pointerX: number; pointerY: number }>()
const canvas = shallowRef<HTMLCanvasElement | null>(null)
const ready = shallowRef(false)
let scene: PrismScene | undefined
let observer: ResizeObserver | undefined
let preference: MediaQueryList | undefined
let disposed = false

function paint() {
  if (document.hidden) return
  const reduce = preference?.matches
  scene?.render({ progress: reduce ? 0 : props.progress, x: reduce ? 0 : props.pointerX, y: reduce ? 0 : props.pointerY })
}
watch(() => [props.progress, props.pointerX, props.pointerY], paint)

function lost(event: Event) { event.preventDefault(); ready.value = false }
function restored() { scene?.resize(); paint(); ready.value = true }

onMounted(async () => {
  preference = window.matchMedia('(prefers-reduced-motion: reduce)')
  preference.addEventListener?.('change', paint)
  document.addEventListener('visibilitychange', paint)
  try {
    const { createPrismScene } = await import('./prismScene')
    if (disposed || !canvas.value) return
    scene = await createPrismScene(canvas.value, background, far)
    if (disposed) { scene.dispose(); return }
    observer = new ResizeObserver(() => scene?.resize())
    observer.observe(canvas.value)
    canvas.value.addEventListener('webglcontextlost', lost)
    canvas.value.addEventListener('webglcontextrestored', restored)
    paint()
    ready.value = true
  } catch {
    // Keep the original artwork and all navigation usable without WebGL.
    ready.value = false
  }
})
onUnmounted(() => {
  disposed = true
  observer?.disconnect()
  preference?.removeEventListener?.('change', paint)
  document.removeEventListener('visibilitychange', paint)
  canvas.value?.removeEventListener('webglcontextlost', lost)
  canvas.value?.removeEventListener('webglcontextrestored', restored)
  scene?.dispose()
})
</script>

<template>
  <div class="prism-scene" :data-scene-ready="ready" aria-hidden="true">
    <img class="scene-art" :src="background" alt="" width="1672" height="941" fetchpriority="high" decoding="async" draggable="false" />
    <canvas ref="canvas" class="depth-canvas" :style="{ opacity: ready ? Math.min(1, progress * 28) : 0 }"></canvas>
  </div>
</template>

<style scoped>
.prism-scene, .scene-art, .depth-canvas { position: absolute; inset: 0; width: 100%; height: 100%; }
.prism-scene { filter: brightness(.92); }
.scene-art { object-fit: cover; object-position: center; }
.depth-canvas { opacity: 0; pointer-events: none; }
</style>
