import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
import { parseChangelog } from './src/lib/release'

export default defineConfig({
  base: '/image-studio/',
  plugins: [react()],
  resolve: { alias: { '@': resolve(__dirname, 'src') } },
  define: {
    __APP_VERSION__: JSON.stringify(readFileSync(resolve(__dirname, 'VERSION'), 'utf8').trim()),
    __APP_RELEASES__: JSON.stringify(parseChangelog(readFileSync(resolve(__dirname, 'CHANGELOG.md'), 'utf8'))),
  },
  build: { outDir: '../public/image-studio', emptyOutDir: true, assetsDir: 'static' },
})
