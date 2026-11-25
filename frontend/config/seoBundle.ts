import type { Plugin } from 'vite'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

// Emits seo.json next to index.html. The backend reads it to give crawlers
// per-page titles, descriptions, structured data and readable body text, and
// to generate robots.txt, sitemap.xml and llms.txt for the requesting host.
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
      // Same merge as src/utils/tutorial.ts: site articles override the generic ones.
      const ownDocs = new Set(own.docs.map(d => d.id))
      const ownFaqs = new Set(own.faqs.map(f => f.id))
      const docs = [...own.docs, ...base.docs.filter(d => !ownDocs.has(d.id)).map(d => ({ ...d, category: d.category === '开始使用' ? '接入基础' : d.category }))]
      const faqs = [...own.faqs, ...base.faqs.filter(f => !ownFaqs.has(f.id))]
      this.emitFile({ type: 'asset', fileName: 'seo.json', source: JSON.stringify({ site, docs, faqs }) })
    },
  }
}
