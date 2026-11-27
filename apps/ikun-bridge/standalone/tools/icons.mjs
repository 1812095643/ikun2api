import { readFile, writeFile } from 'node:fs/promises';

const names = ['log-out', 'monitor', 'play', 'plug', 'refresh-cw', 'settings', 'square', 'terminal', 'ticket', 'undo-2', 'x'];
const symbols = await Promise.all(names.map(async name => {
  const file = new URL(`../../node_modules/lucide-static/icons/${name}.svg`, import.meta.url);
  const svg = await readFile(file, 'utf8');
  const inner = svg.match(/<svg[^>]+>([\s\S]*)<\/svg>/)[1].trim();
  return `<symbol id="${name}" viewBox="0 0 24 24">${inner}</symbol>`;
}));
await writeFile(new URL('../ui/icons.svg', import.meta.url),
  `<!-- Lucide (ISC): see Lucide-LICENSE.txt -->\n<svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">\n${symbols.join('\n')}\n</svg>\n`);
await writeFile(new URL('../ui/Lucide-LICENSE.txt', import.meta.url),
  await readFile(new URL('../../node_modules/lucide-static/LICENSE', import.meta.url)));
