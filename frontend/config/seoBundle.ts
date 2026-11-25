import type { Plugin } from 'vite'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

// 将现有公开教程写入构建产物，供 Go 返回页面正文和站点发现文件。
interface Doc { id: string; category: string; title: string; description: string; body: string; minutes?: number; help?: boolean }
interface Faq { id: string; category: string; question: string; answer: string }

export function seoBundle(root: string): Plugin {
  const read = (name: string) => JSON.parse(readFileSync(resolve(root, 'src/data', name), 'utf8'))
  return {
    name: 'seo-bundle',
    apply: 'build',
    generateBundle() {
      const site = read('seo-site.json')
      const own = read('tutorial-site.json') as { docs: Doc[]; faqs: Faq[] }
      const base = read('tutorial.json') as { docs: Doc[]; faqs: Faq[] }
      // 与现有教程页保持同一覆盖顺序：本站文章优先。
      const ownDocs = new Set(own.docs.map(d => d.id))
      const ownFaqs = new Set(own.faqs.map(f => f.id))
      const docs = [...own.docs, ...base.docs.filter(d => !ownDocs.has(d.id)).map(d => ({ ...d, category: d.category === '开始使用' ? '接入基础' : d.category }))]
      const faqs = [...own.faqs, ...base.faqs.filter(f => !ownFaqs.has(f.id))]
      this.emitFile({ type: 'asset', fileName: 'seo.json', source: JSON.stringify({ site, docs, faqs }) })
    },
  }
}
