import { existsSync } from 'node:fs'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'

const studio = resolve(dirname(fileURLToPath(import.meta.url)), '../studio')
const vite = resolve(studio, 'node_modules/vite/bin/vite.js')
if (!existsSync(vite)) {
  const install = spawnSync('npm', ['ci', '--no-audit', '--no-fund'], {
    cwd: studio, stdio: 'inherit', shell: process.platform === 'win32',
  })
  if (install.status !== 0) process.exit(install.status ?? 1)
}
const build = spawnSync(process.execPath, [vite, 'build'], { cwd: studio, stdio: 'inherit' })
process.exit(build.status ?? 1)
