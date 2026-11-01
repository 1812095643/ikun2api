import { onMounted, onUnmounted, shallowRef, type Ref } from 'vue'

/** 单帧镜头状态。只含普通数值，逐帧更新不经过 Vue 响应式系统。 */
export interface LandingFrame {
  /** 缓动后的镜头推进进度，0–1。 */
  progress: number
  /** 缓动后的指针偏移，-1–1。 */
  x: number
  y: number
  /** 相对首页顶部的实际滚动距离（px），不做缓动。 */
  offset: number
  viewHeight: number
}
export type LandingFrameListener = (frame: Readonly<LandingFrame>) => void

// 滚动只做极短的缓动，让镜头紧贴页面内容；指针视差保留较柔和的缓动。
// 原先两者共用 110ms 时间常数，滚轮滚动时镜头最多落后约 10% 行程，文字与背景脱节造成拖滞感。
const SCROLL_EASE_MS = 40
const POINTER_EASE_MS = 110

/** Event-driven camera movement: no permanent render loop or scroll interception. */
export function useLandingMotion(root: Ref<HTMLElement | null>) {
  let frame = 0
  let lastTime = 0
  let viewHeight = 1
  let rootTop = 0
  let journeyDistance = 1
  let reduced: MediaQueryList | undefined
  let finePointer: MediaQueryList | undefined
  const current = { x: 0, y: 0, scroll: 0 }
  const target = { x: 0, y: 0, scroll: 0 }
  const state: LandingFrame = { progress: 0, x: 0, y: 0, offset: 0, viewHeight: 1 }
  const listeners = new Set<LandingFrameListener>()
  const scrollHintVisible = shallowRef(false)
  let hintTimer: ReturnType<typeof setTimeout> | undefined
  let interacted = false
  let hintRoot: HTMLElement | null = null
  let measured = false
  let measureFrame = 0
  let measureTimer: ReturnType<typeof setTimeout> | undefined

  function dismissHint() {
    interacted = true
    scrollHintVisible.value = false
    if (hintTimer !== undefined) clearTimeout(hintTimer)
    hintTimer = undefined
  }

  function interaction(event: Event) {
    // Let the hint button complete its own click/keyboard activation before removal.
    if ((event.type === 'pointerdown' || event.type === 'touchstart' || event.type === 'keydown') &&
      event.target instanceof Element && event.target.closest('.scroll-cue') &&
      (event.type !== 'keydown' || ['Enter', ' '].includes((event as KeyboardEvent).key))) return
    dismissHint()
  }

  // 性能修复：原实现每帧写入根节点的 4 个 CSS 变量并更新响应式状态，触发整棵子树样式重算和组件重渲染。
  // 现在只把数值交给订阅者，由场景和遮罩各自直接更新所需的单个元素。
  function paint() {
    state.progress = current.scroll
    state.x = current.x
    state.y = current.y
    state.offset = Math.max(0, window.scrollY - rootTop)
    state.viewHeight = viewHeight
    listeners.forEach(listener => listener(state))
  }

  /** 订阅镜头帧；注册时立即以当前状态回调一次，返回取消订阅函数。 */
  function onFrame(listener: LandingFrameListener) {
    listeners.add(listener)
    listener(state)
    return () => { listeners.delete(listener) }
  }

  function stop() {
    if (frame) cancelAnimationFrame(frame)
    frame = 0
    lastTime = 0
  }

  function tick(time: number) {
    frame = 0
    if (document.hidden || reduced?.matches || !root.value) return
    const elapsed = lastTime ? Math.min(time - lastTime, 64) : 16
    lastTime = time
    const scrollBlend = 1 - Math.exp(-elapsed / SCROLL_EASE_MS)
    const pointerBlend = 1 - Math.exp(-elapsed / POINTER_EASE_MS)
    current.scroll += (target.scroll - current.scroll) * scrollBlend
    current.x += (target.x - current.x) * pointerBlend
    current.y += (target.y - current.y) * pointerBlend
    const remaining = Math.abs(target.scroll - current.scroll) + Math.abs(target.x - current.x) + Math.abs(target.y - current.y)
    if (remaining < 0.0005) {
      Object.assign(current, target)
      lastTime = 0
    } else {
      frame = requestAnimationFrame(tick)
    }
    paint()
  }

  function request() {
    if (!frame && !document.hidden && !reduced?.matches) frame = requestAnimationFrame(tick)
  }

  function scroll() {
    // 首次测量前就发生滚动时先测量，避免用占位视口高度算出错误的推进进度。
    if (!measured) return measure()
    if (window.scrollY > rootTop + 4) dismissHint()
    target.scroll = Math.min(1, Math.max(0, (window.scrollY - rootTop) / journeyDistance))
    request()
  }

  function measure() {
    measured = true
    viewHeight = Math.max(1, window.innerHeight)
    rootTop = (root.value?.getBoundingClientRect().top || 0) + window.scrollY
    // 首屏到能力章节的实际距离即镜头行程，四段连续布局后不再依赖旧的 1.8 屏占位。
    journeyDistance = Math.max(1, root.value?.querySelector<HTMLElement>('#home-explore')?.offsetTop || viewHeight)
    scroll()
  }

  function move(event: PointerEvent) {
    if (!finePointer?.matches || reduced?.matches || event.pointerType === 'touch') return
    if (!measured) measure()
    target.x = (event.clientX / Math.max(1, window.innerWidth) - 0.5) * 2
    target.y = (event.clientY / viewHeight - 0.5) * 2
    request()
  }

  function resetPointer() {
    target.x = target.y = 0
    request()
  }

  function motionPreference() {
    if (reduced?.matches) {
      stop()
      current.x = current.y = current.scroll = 0
      paint()
    } else measure()
  }

  function visibility() {
    if (document.hidden) stop()
    else measure()
  }

  function explore() {
    dismissHint()
    root.value?.querySelector<HTMLElement>('#home-explore')?.scrollIntoView({
      behavior: reduced?.matches ? 'instant' : 'smooth', block: 'start'
    })
  }

  onMounted(() => {
    reduced = window.matchMedia('(prefers-reduced-motion: reduce)')
    finePointer = window.matchMedia('(hover: hover) and (pointer: fine)')
    reduced.addEventListener?.('change', motionPreference)
    window.addEventListener('scroll', scroll, { passive: true })
    window.addEventListener('resize', measure, { passive: true })
    document.addEventListener('visibilitychange', visibility)
    root.value?.addEventListener('pointermove', move, { passive: true })
    root.value?.addEventListener('pointerleave', resetPointer)
    hintRoot = root.value
    hintRoot?.addEventListener('wheel', interaction, { passive: true })
    hintRoot?.addEventListener('touchstart', interaction, { passive: true })
    hintRoot?.addEventListener('pointerdown', interaction, { passive: true })
    document.addEventListener('keydown', interaction)
    // 性能修复：挂载时立即读取 getBoundingClientRect 会强制整页同步布局（约 40ms 长任务）。
    // 首帧绘制完成后布局已是最新，此时再测量几乎没有开销。
    measureFrame = requestAnimationFrame(() => {
      measureFrame = 0
      measureTimer = setTimeout(() => {
        measureTimer = undefined
        if (!measured) measure()
      })
    })
    hintTimer = setTimeout(() => {
      hintTimer = undefined
      if (!interacted && window.scrollY <= rootTop + 4) scrollHintVisible.value = true
    }, 1400)
  })

  onUnmounted(() => {
    stop()
    listeners.clear()
    if (measureFrame) cancelAnimationFrame(measureFrame)
    if (measureTimer !== undefined) clearTimeout(measureTimer)
    if (hintTimer !== undefined) clearTimeout(hintTimer)
    hintRoot?.removeEventListener('wheel', interaction)
    hintRoot?.removeEventListener('touchstart', interaction)
    hintRoot?.removeEventListener('pointerdown', interaction)
    document.removeEventListener('keydown', interaction)
    reduced?.removeEventListener?.('change', motionPreference)
    window.removeEventListener('scroll', scroll)
    window.removeEventListener('resize', measure)
    document.removeEventListener('visibilitychange', visibility)
    root.value?.removeEventListener('pointermove', move)
    root.value?.removeEventListener('pointerleave', resetPointer)
  })

  return { explore, onFrame, scrollHintVisible }
}
