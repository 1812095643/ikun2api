import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'node:path'

export default defineConfig({
  root: resolve(process.cwd(), 'monitor-standalone'),
  base: '/ops-monitor-ui/',
  plugins: [vue()],
  resolve: { alias: [ { find: '@/api/admin/monitor', replacement: resolve(process.cwd(), 'monitor-standalone/api.ts') }, { find: '@', replacement: resolve(process.cwd(), 'src') } ] },
  css: { postcss: resolve(process.cwd()) },
  build: { outDir: resolve(process.cwd(), 'monitor-dist'), emptyOutDir: true },
})
