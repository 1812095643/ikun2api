import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, shallowRef } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import { useLandingMotion } from '../useLandingMotion'

describe('landing scroll hint', () => {
  let wrapper: VueWrapper | undefined
  let motion: ReturnType<typeof useLandingMotion>
  let reduce = false
  const scrollIntoView = vi.fn()

  function start() {
    wrapper = mount(defineComponent({
      setup() {
        const root = shallowRef<HTMLElement | null>(null)
        motion = useLandingMotion(root)
        return () => h('div', { ref: root }, [
          h('button', { class: 'scroll-cue', onClick: motion.explore }, 'Explore'),
          h('div', { id: 'home-explore' })
        ])
      }
    }), { attachTo: document.body })
    return wrapper
  }

  beforeEach(() => {
    vi.useFakeTimers()
    reduce = false
    vi.stubGlobal('scrollY', 0)
    vi.stubGlobal('matchMedia', vi.fn((query: string) => ({
      matches: query.includes('reduced-motion') ? reduce : false,
      addEventListener: vi.fn(), removeEventListener: vi.fn()
    })))
    vi.stubGlobal('requestAnimationFrame', vi.fn(() => 1))
    vi.stubGlobal('cancelAnimationFrame', vi.fn())
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => ({
      x: 0, y: -window.scrollY, top: -window.scrollY, left: 0, bottom: 941 - window.scrollY,
      right: 1672, width: 1672, height: 941, toJSON: () => ({})
    }))
    Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: scrollIntoView })
    scrollIntoView.mockReset()
  })
  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('appears after a short idle delay', () => {
    start()
    expect(motion.scrollHintVisible.value).toBe(false)
    vi.advanceTimersByTime(1399)
    expect(motion.scrollHintVisible.value).toBe(false)
    vi.advanceTimersByTime(1)
    expect(motion.scrollHintVisible.value).toBe(true)
  })

  it.each(['wheel', 'touchstart', 'pointerdown'])('cancels the prompt before it appears on %s', (event) => {
    start().element.dispatchEvent(new Event(event, { bubbles: true }))
    vi.advanceTimersByTime(3000)
    expect(motion.scrollHintVisible.value).toBe(false)
  })

  it('dismisses after scrolling and stays hidden when returning to the top', () => {
    start()
    vi.advanceTimersByTime(1400)
    vi.stubGlobal('scrollY', 120)
    window.dispatchEvent(new Event('scroll'))
    expect(motion.scrollHintVisible.value).toBe(false)
    vi.stubGlobal('scrollY', 0)
    window.dispatchEvent(new Event('scroll'))
    vi.advanceTimersByTime(10000)
    expect(motion.scrollHintVisible.value).toBe(false)
  })

  it('stays hidden when the page is opened at a restored scroll position', () => {
    vi.stubGlobal('scrollY', 400)
    start()
    vi.advanceTimersByTime(3000)
    expect(motion.scrollHintVisible.value).toBe(false)
  })

  it('dismisses after keyboard interaction', () => {
    start()
    vi.advanceTimersByTime(1400)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab' }))
    expect(motion.scrollHintVisible.value).toBe(false)
  })

  it('keeps the hint available through pointerdown and completes its scroll action', async () => {
    const button = start().get('.scroll-cue')
    vi.advanceTimersByTime(1400)
    await button.trigger('pointerdown')
    await button.trigger('touchstart')
    expect(motion.scrollHintVisible.value).toBe(true)
    await button.trigger('click')
    expect(motion.scrollHintVisible.value).toBe(false)
    expect(scrollIntoView).toHaveBeenCalledWith({ behavior: 'smooth', block: 'start' })
  })

  it('uses an instant action when reduced motion is requested', () => {
    reduce = true
    start()
    vi.advanceTimersByTime(1400)
    motion.explore()
    expect(scrollIntoView).toHaveBeenCalledWith({ behavior: 'instant', block: 'start' })
  })

  it('clears its timer and event listeners on unmount', () => {
    const remove = vi.spyOn(document, 'removeEventListener')
    start()
    wrapper!.unmount()
    wrapper = undefined
    expect(vi.getTimerCount()).toBe(0)
    expect(remove).toHaveBeenCalledWith('keydown', expect.any(Function))
  })
})
