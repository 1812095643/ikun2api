import { onMounted, onUnmounted, shallowRef, type Ref } from 'vue'

/** Event-driven camera movement: no permanent render loop or scroll interception. */
export function useLandingMotion(root: Ref<HTMLElement | null>) {
  let frame = 0
  let lastTime = 0
  let viewHeight = 1
  let rootTop = 0
  let reduced: MediaQueryList | undefined
  let finePointer: MediaQueryList | undefined
  const current = { x: 0, y: 0, scroll: 0 }
  const target = { x: 0, y: 0, scroll: 0 }
  const progress = shallowRef(0)
  const pointerX = shallowRef(0)
  const pointerY = shallowRef(0)
  const scrollHintVisible = shallowRef(false)
  let hintTimer: ReturnType<typeof setTimeout> | undefined
  let interacted = false
  let hintRoot: HTMLElement | null = null

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

  function paint() {
    const style = root.value?.style
    if (!style) return
    progress.value = current.scroll
    pointerX.value = current.x
    pointerY.value = current.y
    style.setProperty('--pointer-x', current.x.toFixed(4))
    style.setProperty('--pointer-y', current.y.toFixed(4))
    style.setProperty('--journey', current.scroll.toFixed(4))
    style.setProperty('--overscan', (Math.max(Math.abs(current.x), Math.abs(current.y)) * .018).toFixed(4))
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
    const blend = 1 - Math.exp(-elapsed / 110)
    let remaining = 0
    for (const key of ['x', 'y', 'scroll'] as const) {
      current[key] += (target[key] - current[key]) * blend
      remaining += Math.abs(target[key] - current[key])
    }
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
    if (window.scrollY > rootTop + 4) dismissHint()
    target.scroll = Math.min(1, Math.max(0, (window.scrollY - rootTop) / (viewHeight * 1.8)))
    request()
  }

  function measure() {
    viewHeight = Math.max(1, window.innerHeight)
    rootTop = (root.value?.getBoundingClientRect().top || 0) + window.scrollY
    scroll()
  }

  function move(event: PointerEvent) {
    if (!finePointer?.matches || reduced?.matches || event.pointerType === 'touch') return
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
    measure()
    hintTimer = setTimeout(() => {
      hintTimer = undefined
      if (!interacted && window.scrollY <= rootTop + 4) scrollHintVisible.value = true
    }, 1400)
  })

  onUnmounted(() => {
    stop()
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

  return { explore, progress, pointerX, pointerY, scrollHintVisible }
}
