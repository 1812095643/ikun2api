import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick, onMounted, ref, shallowRef } from 'vue'
import { mount } from '@vue/test-utils'

const mounts = { sidebar: 0, header: 0 }
const pageChanges = { count: 0 }

vi.mock('@/stores', () => ({ useAppStore: () => ({ sidebarCollapsed: false }) }))
vi.mock('../AppSidebar.vue', async () => {
  const { onLayoutPageChange } = await import('../layoutShell')
  return {
    default: defineComponent({
      setup() {
        onMounted(() => { mounts.sidebar++ })
        onLayoutPageChange(() => { pageChanges.count++ })
        return () => h('aside', { 'data-test': 'sidebar' })
      },
    }),
  }
})
vi.mock('../AppHeader.vue', () => ({
  default: defineComponent({
    setup() {
      onMounted(() => { mounts.header++ })
      return () => h('header', { 'data-test': 'header' })
    },
  }),
}))

const { default: PersistentLayout } = await import('../PersistentLayout.vue')
const { default: AppLayout } = await import('../AppLayout.vue')

const pageA = defineComponent({ setup: () => () => h(AppLayout, { class: 'page-a' }, () => h('p', 'A')) })
const pageB = defineComponent({ setup: () => () => h(AppLayout, { class: 'page-b' }, () => h('p', 'B')) })
const plain = defineComponent({ setup: () => () => h('section', { class: 'login' }, 'login') })

function mountShell() {
  const current = shallowRef<unknown>(pageA)
  const param = ref(0)
  const wrapper = mount(defineComponent({
    setup: () => () => h(PersistentLayout, null, () => h(current.value as never, { key: undefined, param: param.value })),
  }))
  return { wrapper, current, param }
}

describe('PersistentLayout', () => {
  it('keeps sidebar and header mounted across layout pages and forwards page classes', async () => {
    mounts.sidebar = 0; mounts.header = 0; pageChanges.count = 0
    const { wrapper, current, param } = mountShell()
    await nextTick()
    expect(wrapper.find('[data-test="sidebar"]').exists()).toBe(true)
    expect(wrapper.find('[role="main"]').attributes('data-tour')).toBe('user-page-content')
    expect(wrapper.element.firstElementChild?.classList.contains('page-a')).toBe(true)

    current.value = pageB
    await nextTick(); await nextTick()
    expect(wrapper.text()).toContain('B')
    expect(wrapper.element.firstElementChild?.classList.contains('page-b')).toBe(true)
    expect(wrapper.element.firstElementChild?.classList.contains('page-a')).toBe(false)
    expect(mounts).toEqual({ sidebar: 1, header: 1 })
    expect(pageChanges.count).toBe(1)

    // 同一页面实例更新（如路由参数变化）不算换页，与原来不重建的行为一致
    param.value++
    await nextTick()
    expect(pageChanges.count).toBe(1)
  })

  it('renders pages without AppLayout bare, and brings the shell back for layout pages', async () => {
    mounts.sidebar = 0; mounts.header = 0
    const { wrapper, current } = mountShell()
    await nextTick()
    current.value = plain
    await nextTick(); await nextTick()
    expect(wrapper.find('[data-test="sidebar"]').exists()).toBe(false)
    expect(wrapper.find('[role="main"]').exists()).toBe(false)
    expect(wrapper.element.firstElementChild?.className).toBe('contents')
    expect(wrapper.find('section.login').exists()).toBe(true)

    current.value = pageA
    await nextTick(); await nextTick()
    expect(wrapper.find('[data-test="sidebar"]').exists()).toBe(true)
    expect(mounts.sidebar).toBe(2)
  })

  it('AppLayout without a persistent shell still renders the full layout', () => {
    mounts.sidebar = 0
    const wrapper = mount(pageA)
    expect(wrapper.find('[data-test="sidebar"]').exists()).toBe(true)
    expect(wrapper.find('main').attributes('data-tour')).toBe('user-page-content')
    expect(wrapper.classes()).toContain('page-a')
    expect(mounts.sidebar).toBe(1)
  })
})
