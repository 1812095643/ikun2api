import { beforeEach, describe, expect, it } from 'vitest'
import { updateFavicon } from '@/utils/branding'

describe('updateFavicon', () => {
  beforeEach(() => {
    document.head.innerHTML = '<link rel="icon" href="/logo.svg">'
  })

  it('replaces the default favicon with the configured logo', () => {
    updateFavicon('https://example.com/custom-logo.png')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.href).toBe('https://example.com/custom-logo.png')
  })

  it('ignores unsafe logo URLs', () => {
    updateFavicon('javascript:alert(1)')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.getAttribute('href')).toBe('/logo.svg')
  })
})

// User-configured names are data. Rich text and exported configuration must escape them per format.
import { formatSiteBranding, resolveSiteName } from '@/utils/branding'
describe('custom site branding', () => {
 it('uses a single default and trims configured names', () => { expect(resolveSiteName('  青云 AI  ')).toBe('青云 AI'); expect(resolveSiteName('  ')).toBe('IKUN2api') })
 it('escapes a configured name inside rich tutorial text', () => { expect(formatSiteBranding('<b>__SITE_NAME_HTML__</b>', '<img src=x onerror=alert(1)> & AI')).toBe('<b>&lt;img src=x onerror=alert(1)&gt; &amp; AI</b>') })
 it('keeps plain-text UI names literal', () => { expect(formatSiteBranding('__SITE_NAME__ 门户', 'A & B')).toBe('A & B 门户') })
})
