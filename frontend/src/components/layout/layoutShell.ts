import { computed, inject, shallowRef, watch, type InjectionKey, type StyleValue } from 'vue'

/**
 * 常驻后台外框（侧边栏 + 顶栏）的共享状态。
 *
 * 各页面仍在模板根部写 <AppLayout>；当外层存在 PersistentLayout 时，AppLayout 只登记自己并渲染内容，
 * 侧边栏和顶栏由 PersistentLayout 常驻渲染，切换菜单不再整块卸载重建。
 * pageGeneration 在“换了一个页面实例”时递增，对应原来外框随页面重建的时机，
 * 外框内组件据此复位下拉框、重跑原挂载时的刷新，行为与重建一致。
 */
export interface LayoutShell {
  readonly active: boolean
  readonly rootClass: unknown
  readonly rootStyle: StyleValue | undefined
  readonly pageGeneration: number
  register(attrs: LayoutAttrs): number
  update(id: number, attrs: LayoutAttrs): void
  unregister(id: number): void
}

export interface LayoutAttrs {
  class?: unknown
  style?: StyleValue
}

export const LAYOUT_SHELL: InjectionKey<LayoutShell> = Symbol('layout-shell')

export function createLayoutShell(): LayoutShell {
  let nextID = 0
  // 只整体替换数组，不做深层响应式（class/style 值可能是复杂对象）
  const pages = shallowRef<{ id: number; attrs: LayoutAttrs }[]>([])
  const generation = shallowRef(0)
  const current = computed(() => pages.value[pages.value.length - 1])
  return {
    get active() { return pages.value.length > 0 },
    get rootClass() { return current.value?.attrs.class },
    get rootStyle() { return current.value?.attrs.style },
    get pageGeneration() { return generation.value },
    register(attrs) {
      const id = ++nextID
      pages.value = [...pages.value, { id, attrs }]
      generation.value++
      return id
    },
    update(id, attrs) {
      const page = pages.value.find((item) => item.id === id)
      if (!page || (page.attrs.class === attrs.class && page.attrs.style === attrs.style)) return
      pages.value = pages.value.map((item) => (item.id === id ? { id, attrs } : item))
    },
    unregister(id) {
      pages.value = pages.value.filter((item) => item.id !== id)
    },
  }
}

/** 外框内组件：页面实例切换时调用 callback（无常驻外框时不触发，组件本身就会随页面重建）。 */
export function onLayoutPageChange(callback: () => void): void {
  const shell = inject(LAYOUT_SHELL, null)
  if (!shell) return
  watch(() => shell.pageGeneration, callback)
}
