import { sanitizeUrl } from '@/utils/url'

export const DEFAULT_SITE_NAME = 'IKUN2api'
export function resolveSiteName(value: unknown): string {
  return typeof value === 'string' && value.trim() ? value.trim() : DEFAULT_SITE_NAME
}
export function formatSiteBranding(message: string, value: unknown): string {
  const name = resolveSiteName(value)
  const html = name.replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!)
  return message.split('__SITE_NAME_HTML__').join(html).split('__SITE_NAME__').join(name)
}

export function updateFavicon(logoUrl: string): void {
  const sanitizedLogoUrl = sanitizeUrl(logoUrl, {
    allowRelative: true,
    allowDataUrl: true,
  })
  if (!sanitizedLogoUrl) {
    return
  }

  let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (!link) {
    link = document.createElement('link')
    link.rel = 'icon'
    document.head.appendChild(link)
  }

  link.type = sanitizedLogoUrl.endsWith('.svg') ? 'image/svg+xml' : 'image/x-icon'
  link.href = sanitizedLogoUrl
}
