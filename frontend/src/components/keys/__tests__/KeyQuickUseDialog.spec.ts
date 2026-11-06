import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import KeyQuickUseDialog from '../KeyQuickUseDialog.vue'
import type { ApiKey } from '@/types'

vi.mock('vue-i18n', async () => ({ ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
const key = (id = 1, restricted = false) => ({ id, key: 'sk-synthetic', group: { platform: 'anthropic', claude_code_only: restricted } }) as ApiKey
const render = (apiKey = key()) => mount(KeyQuickUseDialog, {
  attachTo: document.body,
  props: { apiKey, baseUrl: 'https://api.example.com', siteName: 'Test' },
  global: { stubs: {
    Teleport: true, Icon: true,
    CcSwitchImportDialog: { props: ['apiKey'], emits: ['back', 'close'], template: '<div data-test="ccs"><button data-test="back" @click="$emit(\'back\')">Back</button><button data-test="close" @click="$emit(\'close\')">Close</button></div>' },
    MagpieImportDialog: { props: ['apiKey'], emits: ['back', 'close'], template: '<div data-test="magpie"><button data-test="back" @click="$emit(\'back\')">Back</button></div>' }
  } }
})

describe('Quick Use method selection', () => {
  it('opens only the method list first, then only the selected configuration dialog', async () => {
    const w = render()
    expect(w.find('[data-test="ccs"]').exists()).toBe(false)
    expect(w.find('[data-test="magpie"]').exists()).toBe(false)
    await w.get('[data-test="choose-magpie"]').trigger('click')
    expect(w.find('[data-test="magpie"]').exists()).toBe(true)
    expect(w.find('[data-test="choose-ccswitch"]').exists()).toBe(false)
    await w.get('[data-test="back"]').trigger('click')
    await w.get('[data-test="choose-ccswitch"]').trigger('click')
    expect(w.find('[data-test="ccs"]').exists()).toBe(true)
    expect(w.find('[data-test="magpie"]').exists()).toBe(false)
    await w.get('[data-test="close"]').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
    w.unmount()
  })
  it('returns to the method list when changing keys', async () => {
    const w = render()
    await w.get('[data-test="choose-magpie"]').trigger('click')
    await w.setProps({ apiKey: key(2) })
    expect(w.find('[data-test="choose-magpie"]').exists()).toBe(true)
    expect(w.find('[data-test="magpie"]').exists()).toBe(false)
    w.unmount()
  })
  it('keeps CC Switch available for CLI-only groups without enabling Magpie', () => {
    const w = render(key(1, true))
    expect(w.get('[data-test="choose-magpie"]').attributes('disabled')).toBeDefined()
    expect(w.get('[data-test="choose-ccswitch"]').attributes('disabled')).toBeUndefined()
    w.unmount()
  })
})


it('supports arrow navigation, Escape, outside clicks and bottom-edge placement', async () => {
  const anchor = document.createElement('button')
  anchor.getBoundingClientRect = () => ({ top: 750, bottom: 780, left: 1000, right: 1040, height: 30, width: 40, x: 1000, y: 750, toJSON() {} })
  document.body.append(anchor)
  const w = render()
  await w.setProps({ anchor }); await flushPromises()
  const menu = w.get('[data-test="quick-use-menu"]')
  expect(menu.attributes('style')).toContain('bottom:')
  ;(w.get('[data-test="choose-ccswitch"]').element as HTMLButtonElement).focus()
  expect(document.activeElement?.textContent).toBe('keys.importToCcSwitch')
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' }))
  expect(document.activeElement?.textContent).toContain('keys.magpieImport.title')
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
  expect(w.emitted('close')).toHaveLength(1)
  document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
  expect(w.emitted('close')).toHaveLength(2)
  w.unmount(); anchor.remove()
})
