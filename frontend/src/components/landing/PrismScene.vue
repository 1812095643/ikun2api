<script setup lang="ts">
import { onMounted, onUnmounted, shallowRef } from 'vue'
import background from '@/assets/landing/prism-gate.webp'
import far from '@/assets/landing/prism-gate-far.webp'
import type { LandingFrame, LandingFrameListener } from '@/composables/useLandingMotion'
import type { PrismScene } from './prismScene'

const props = defineProps<{ subscribe: (listener: LandingFrameListener) => () => void }>()
const canvas = shallowRef<HTMLCanvasElement | null>(null)
const ready = shallowRef(false)
const idleSupported = typeof window !== 'undefined' && typeof window.requestIdleCallback === 'function'
let element: HTMLCanvasElement | null = null
let scene: PrismScene | undefined
let observer: ResizeObserver | undefined
let reduced: MediaQueryList | undefined
let unsubscribe: (() => void) | undefined
let idleHandle: number | undefined
let building = false
let disposed = false
// 初始化失败（例如浏览器禁用 WebGL）后不再由滚动帧反复重试，只在上下文恢复时再试。
let failed = false
let controller: AbortController | undefined
// 上下文丢失时递增；构建期间发生丢失则丢弃本次结果，等恢复后重建。
let contextGeneration = 0
let contextLost = false
// 最近一帧的镜头状态，以及画布上实际绘制的状态；两者一致时不重复渲染。
const latest = { progress: 0, x: 0, y: 0 }
const drawn = { progress: Number.NaN, x: Number.NaN, y: Number.NaN }
let opacity = -1

function draw(force = false) {
  if (!element) return
  // 画布在推进初段淡入、覆盖静态原图；完全透明时不渲染。
  const next = ready.value ? Math.min(1, latest.progress * 28) : 0
  if (next !== opacity) {
    opacity = next
    element.style.opacity = String(next)
  }
  if (!scene || next === 0 || document.hidden) return
  if (!force && drawn.progress === latest.progress && drawn.x === latest.x && drawn.y === latest.y) return
  Object.assign(drawn, latest)
  scene.render(latest)
}

/** 画面被清空（尺寸变化、重建）后标记为未绘制，下一次可见时必定重绘。 */
function invalidate() {
  drawn.progress = drawn.x = drawn.y = Number.NaN
}

function onFrame(frame: Readonly<LandingFrame>) {
  latest.progress = frame.progress
  latest.x = frame.x
  latest.y = frame.y
  // 空闲构建之前用户就开始滚动时立即构建，避免镜头推进被延后。
  if (!scene && frame.progress > 0) start()
  draw()
}

function resized() {
  if (!scene) return
  scene.resize()
  invalidate()
  draw()
}

function cancelIdle() {
  if (idleHandle === undefined) return
  if (idleSupported) window.cancelIdleCallback(idleHandle)
  else window.clearTimeout(idleHandle)
  idleHandle = undefined
}

function blocked() {
  return Boolean(scene || building || disposed || failed || contextLost || reduced?.matches || !element)
}

function schedule() {
  if (blocked() || idleHandle !== undefined) return
  // 静态原图先完成首屏绘制，空闲时再加载 Three.js 与纹理，不与首屏渲染争抢主线程。
  idleHandle = idleSupported
    ? window.requestIdleCallback(() => { idleHandle = undefined; start() }, { timeout: 1500 })
    : window.setTimeout(() => { idleHandle = undefined; start() }, 400)
}

function start() {
  cancelIdle()
  void build()
}

async function build() {
  if (blocked()) return
  building = true
  const generation = contextGeneration
  const abort = new AbortController()
  controller = abort
  try {
    const { createPrismScene } = await import('./prismScene')
    // 模块求值与创建 WebGL 上下文分到两个任务，避免合成一个长任务。
    await new Promise<void>(resolve => setTimeout(resolve))
    if (abort.signal.aborted || !element) return
    const created = await createPrismScene(element, background, far, abort.signal)
    if (abort.signal.aborted || generation !== contextGeneration) {
      created.dispose(disposed)
      return
    }
    scene = created
    ready.value = true
    // 异步构建期间发生的尺寸变化不会触发 resize，接管前补一次同步。
    scene.resize()
    invalidate()
    draw(true)
  } catch {
    // Keep the original artwork and all navigation usable without WebGL.
    if (!abort.signal.aborted && generation === contextGeneration) failed = true
    ready.value = false
  } finally {
    if (controller === abort) controller = undefined
    building = false
    // 构建被上下文丢失或偏好切换打断、且条件已恢复时（期间的 restored/change 事件被跳过），补一次调度。
    if (abort.signal.aborted || generation !== contextGeneration) schedule()
  }
}

function release() {
  controller?.abort()
  scene?.dispose()
  scene = undefined
  ready.value = false
  invalidate()
  draw()
}

function preferenceChanged() {
  // 减少动态效果时画布始终不可见，取消在途构建并释放资源；关闭该偏好后重新按空闲调度构建。
  if (reduced?.matches) {
    cancelIdle()
    release()
  } else schedule()
}

function lost(event: Event) {
  event.preventDefault()
  contextGeneration++
  contextLost = true
  cancelIdle()
  // 纹理源已在上传后释放，上下文恢复后整体重建场景。
  release()
}

function restored() {
  contextLost = false
  failed = false
  schedule()
}

onMounted(() => {
  element = canvas.value
  reduced = window.matchMedia('(prefers-reduced-motion: reduce)')
  reduced.addEventListener?.('change', preferenceChanged)
  element?.addEventListener('webglcontextlost', lost)
  element?.addEventListener('webglcontextrestored', restored)
  if (element && typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(resized)
    observer.observe(element)
  }
  unsubscribe = props.subscribe(onFrame)
  schedule()
})

onUnmounted(() => {
  disposed = true
  cancelIdle()
  // 取消在途构建：图片请求立即中止，迟到的结果在构建流程内释放。
  controller?.abort()
  unsubscribe?.()
  observer?.disconnect()
  reduced?.removeEventListener?.('change', preferenceChanged)
  element?.removeEventListener('webglcontextlost', lost)
  element?.removeEventListener('webglcontextrestored', restored)
  // 画布即将移除，立即归还 GPU 上下文。
  scene?.dispose(true)
  scene = undefined
  element = null
})
</script>

<template>
  <div class="prism-scene" :data-scene-ready="ready" aria-hidden="true">
    <img class="scene-art" :src="background" alt="" width="1672" height="941" fetchpriority="high" decoding="async" draggable="false" />
    <canvas ref="canvas" class="depth-canvas"></canvas>
  </div>
</template>

<style scoped>
.prism-scene, .scene-art, .depth-canvas { position: absolute; inset: 0; width: 100%; height: 100%; }
/* 原先对整层（含 WebGL 画布）使用 filter: brightness(.92)，画布每帧更新都要重走一次离屏滤镜。
   8% 黑色叠层在合成阶段得到相同的 0.92 倍亮度，只多一次纯色混合。 */
.prism-scene::after { content: ''; position: absolute; inset: 0; background: #000; opacity: .08; pointer-events: none; }
.scene-art { object-fit: cover; object-position: center; }
.depth-canvas { opacity: 0; pointer-events: none; transition: opacity 360ms ease; }
@media (prefers-reduced-motion: reduce) { .depth-canvas { transition: none; } }
</style>
