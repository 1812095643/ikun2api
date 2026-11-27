import { copyFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';

const dir = path => fileURLToPath(new URL(path, import.meta.url));
export default defineConfig({
  root: dir('./ui'),
  build: { outDir: dir('./dist'), emptyOutDir: true, target: 'es2022' },
  plugins: [{
    name: 'bridge-icons',
    async writeBundle() {
      await copyFile(dir('./ui/icons.svg'), dir('./dist/icons.svg'));
      await copyFile(dir('./ui/Lucide-LICENSE.txt'), dir('./dist/Lucide-LICENSE.txt'));
    },
  }],
});
