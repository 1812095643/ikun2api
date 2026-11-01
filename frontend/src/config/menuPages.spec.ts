import { describe, expect, it } from 'vitest'
import { filterVisibleMenus, isMenuVisible, menuPageGroups } from './menuPages'

describe('menu visibility', () => {
  it('keeps existing installations visible by default and normalizes standalone paths', () => {
    expect(isMenuVisible('/image-studio/')).toBe(true)
    expect(isMenuVisible('/image-studio/?tab=canvas', { '/image-studio': false })).toBe(false)
  })
  it('filters nested admin sections without mutating navigation definitions', () => {
    const input = [{ path: '/admin/channels', children: [{ path: '/admin/channels/pricing' }, { path: '/admin/channels/monitor' }] }]
    const visible = filterVisibleMenus(input, { '/admin/channels/pricing': false })
    expect(visible[0].children).toEqual([{ path: '/admin/channels/monitor' }])
    expect(input[0].children).toHaveLength(2)
    expect(filterVisibleMenus(input, { '/admin/channels': false })).toEqual([])
    expect(filterVisibleMenus(input, { '/admin/channels/pricing': false, '/admin/channels/monitor': false })).toEqual([])
  })
  it('lists canonical pages once and retires the old model plaza', () => {
    const paths = menuPageGroups.flatMap(group => group.pages.map(page => page.path))
    expect(new Set(paths).size).toBe(paths.length)
    expect(paths).toContain('/model-square')
    expect(paths).not.toContain('/model-plaza')
  })
})
