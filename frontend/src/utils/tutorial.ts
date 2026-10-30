import DOMPurify from 'dompurify'
import content from '@/data/tutorial.json'
import siteContent from '@/data/tutorial-site.json'
import { sanitizeUrl } from '@/utils/url'

export interface TutorialDocument {
  id: string
  category: string
  title: string
  description: string
  body: string
  minutes?: number
  help?: boolean
}
export interface TutorialFaq {
  id: string
  category: string
  question: string
  answer: string
}

const siteDocumentIds = new Set(siteContent.docs.map(item => item.id))
const siteFaqIds = new Set(siteContent.faqs.map(item => item.id))
export const tutorialDocuments: TutorialDocument[] = [
  ...siteContent.docs,
  ...content.docs.filter(item => !siteDocumentIds.has(item.id)).map(item => ({
    ...item,
    category: item.category === '开始使用' ? '接入基础' : item.category
  }))
]
export const tutorialFaqs: TutorialFaq[] = [
  ...siteContent.faqs,
  ...content.faqs.filter(item => !siteFaqIds.has(item.id))
]
export const tutorialFeedback = content.feedback

const documentIds = new Set(tutorialDocuments.map(item => item.id))
const localPages: Record<string, string> = {
  '/custom/tickets': '/tickets',
  '/donate': '/purchase',
  '/status-detection': '/monitor'
}
const sitePages = new Set([
  '/dashboard', '/keys', '/usage', '/register', '/login', '/forgot-password',
  '/redeem', '/purchase', '/profile', '/model-square', '/monitor', '/tickets',
  '/home', '/orders', '/subscriptions', '/affiliate', '/available-channels',
  '/image-studio', '/batch-image', '/news', '/reset-monitor', '/lottery', '/checkin'
])

export function tutorialHref(id = 'start', section = ''): string {
  return `/tutorial/${encodeURIComponent(id)}${section ? `#${encodeURIComponent(section)}` : ''}`
}

export function tutorialApiBase(configured: string, origin: string): string {
  return (sanitizeUrl(configured) || origin).replace(/\/+$/, '').replace(/\/v1$/, '')
}

function escapeHtml(value: string): string {
  return value.replace(/[&<>"']/g, character => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
  })[character] || character)
}

export function tutorialText(html: string): string {
  return new DOMParser().parseFromString(html, 'text/html').body.textContent?.replace(/\s+/g, ' ').trim() || ''
}

export function tutorialHtml(html: string, apiBase: string, siteName: string): string {
  const template = document.createElement('template')
  template.innerHTML = DOMPurify.sanitize(
    html.split('__IKUN_API_BASE__').join(escapeHtml(apiBase)).split('__IKUN_SITE_NAME__').join(escapeHtml(siteName)),
    { USE_PROFILES: { html: true, svg: true }, FORBID_TAGS: ['style', 'form', 'input'] }
  )

  // 旧教程中的站点入口统一指向当前站点；业务页面另开标签，保留文档阅读位置。
  template.content.querySelectorAll<HTMLAnchorElement>('a[href]').forEach(anchor => {
    const href = anchor.getAttribute('href') || ''
    const [articleId, section] = href.slice(1).split('/')
    if (href.startsWith('#') && documentIds.has(articleId)) {
      anchor.setAttribute('href', tutorialHref(articleId, section))
    } else if (href.startsWith(`${apiBase}/`)) {
      const pathname = href.slice(apiBase.length).replace(/\/$/, '')
      const localPath = localPages[pathname] || pathname
      if (sitePages.has(localPath)) {
        anchor.setAttribute('href', localPath)
      } else if (pathname === '/tutorial/ccswitch-claude-desktop') {
        anchor.setAttribute('href', 'https://subapi.xiaoye.lol/tutorial/ccswitch-claude-desktop/')
      }
    }
    const destination = anchor.getAttribute('href') || ''
    if (destination === '/tutorial' || destination.startsWith('/tutorial/')) {
      anchor.removeAttribute('target')
      anchor.removeAttribute('rel')
    } else if (destination.startsWith('/') && !destination.startsWith('//')) {
      anchor.target = '_blank'
      anchor.rel = 'noopener noreferrer'
    } else if (anchor.target === '_blank') {
      anchor.rel = 'noopener noreferrer'
    }
  })
  return template.innerHTML
}
