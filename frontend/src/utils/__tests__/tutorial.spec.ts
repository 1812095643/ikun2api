import { describe, expect, it } from 'vitest'
import { tutorialDocuments, tutorialFaqs, tutorialHtml } from '../tutorial'

const apiBase = 'https://example.test'
const render = (body: string) => new DOMParser().parseFromString(tutorialHtml(body, apiBase, '测试站点'), 'text/html')

describe('使用文档链接与内容安全', () => {
  it('所有文章和常见问题引用的教程章节均存在', () => {
    const articles = new Map(tutorialDocuments.map(item => [item.id, item]))
    expect(articles.size).toBe(tutorialDocuments.length)
    expect(new Set(tutorialFaqs.map(item => item.id)).size).toBe(tutorialFaqs.length)
    for (const source of [...tutorialDocuments.map(item => item.body), ...tutorialFaqs.map(item => item.answer)]) {
      for (const link of render(source).querySelectorAll<HTMLAnchorElement>('a[href^="/tutorial/"]')) {
        const url = new URL(link.href, window.location.origin)
        const id = decodeURIComponent(url.pathname.split('/')[2])
        const article = articles.get(id)
        expect(article, link.getAttribute('href') || '').toBeDefined()
        if (url.hash) {
          const section = decodeURIComponent(url.hash.slice(1))
          const exists = article?.help
            ? tutorialFaqs.some(item => `faq-${item.id}` === section)
            : article && render(article.body).getElementById(section)
          expect(Boolean(exists), link.getAttribute('href') || '').toBe(true)
        }
        expect(link.hasAttribute('target')).toBe(false)
      }
    }
  })

  it('控制台业务入口在新标签页打开并保留当前站点', () => {
    const body = render('<a href="/redeem">兑换</a><a href="__IKUN_API_BASE__/keys">密钥</a><a href="#account">注册教程</a>')
    const links = [...body.querySelectorAll('a')]
    expect(links.map(link => link.getAttribute('href'))).toEqual(['/redeem', '/keys', '/tutorial/account'])
    for (const link of links.slice(0, 2)) {
      expect(link.target).toBe('_blank')
      expect(link.rel).toBe('noopener noreferrer')
    }
    expect(links[2].target).toBe('')
  })

  it('清除可执行 HTML，并转义站点名称和请求地址', () => {
    const body = render('<script>alert(1)</script><a href="javascript:alert(1)">链接</a><img src="x" onerror="alert(1)">')
    expect(body.querySelector('script')).toBeNull()
    expect(body.querySelector('[onerror]')).toBeNull()
    expect(body.querySelector('a')?.hasAttribute('href')).toBe(false)
    const html = tutorialHtml('<p>__IKUN_SITE_NAME__</p><code>__IKUN_API_BASE__</code>', '<img src=x onerror=alert(1)>', '<script>alert(1)</script>')
    expect(new DOMParser().parseFromString(html, 'text/html').querySelector('script, img')).toBeNull()
  })
})
