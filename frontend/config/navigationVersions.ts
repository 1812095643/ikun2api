import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import ts from 'typescript'
import type { Plugin } from 'vite'

export function navigationVersions(build: string): Plugin {
  return {
    name: 'navigation-page-versions',
    apply: 'build',
    generateBundle() {
      const pages: Record<string, string> = {}
      const sourceRoot = resolve(process.cwd(), 'src').replaceAll('\\', '/')
      const routes = ['router/index.ts', 'router/workspace.ts']
      const targets = new Map<string, string>()
      for (const route of routes) {
        const code = readFileSync(resolve(sourceRoot, route), 'utf8')
        const ast = ts.createSourceFile(route, code, ts.ScriptTarget.Latest, true)
        const visit = (node: ts.Node) => {
          if (ts.isObjectLiteralExpression(node)) {
            let path = '', component = ''
            for (const property of node.properties) {
              if (!ts.isPropertyAssignment(property)) continue
              const name = property.name.getText(ast).replaceAll(/['"]/g, '')
              if (name === 'path' && ts.isStringLiteral(property.initializer)) path = property.initializer.text
              if (name === 'component') component = property.initializer.getText(ast).match(/import\(['"]@\/([^'"]+)['"]\)/)?.[1] || ''
            }
            if (path.startsWith('/') && !path.startsWith('/:') && component) targets.set(path.replace(/\/:.*$/, '') || '/', `${sourceRoot}/${component}`)
          }
          ts.forEachChild(node, visit)
        }
        visit(ast)
      }
      for (const [path, entry] of targets) {
        const files = new Set<string>()
        const visited = new Set<string>()
        const collect = (id: string) => {
          if (visited.has(id)) return
          visited.add(id)
          const file = id.split('?')[0].replaceAll('\\', '/')
          if (!file.startsWith(`${sourceRoot}/`) || /\/(?:stores|router|i18n|components\/(?:layout|common|icons)|composables\/useSidebarNotices)/.test(file) || /\/api\/client\./.test(file)) return
          files.add(file)
          const info = this.getModuleInfo(id)
          for (const child of [...(info?.importedIds || []), ...(info?.dynamicallyImportedIds || [])]) collect(child)
        }
        collect(entry)
        const hash = createHash('sha256')
        for (const file of [...files].sort()) {
          hash.update(file.slice(sourceRoot.length))
          hash.update(readFileSync(file))
        }
        pages[path] = hash.digest('hex').slice(0, 20)
      }
      this.emitFile({ type: 'asset', fileName: 'navigation-pages.json', source: JSON.stringify({ build, pages }) })
    }
  }
}
