import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmdirSync, unlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const lld = process.env.IKUN_DARWIN_LLD;
if (!lld) throw new Error('Set IKUN_DARWIN_LLD to the Rust toolchain linker');
const scratch = mkdtempSync(join(tmpdir(), 'ikun-ld64-'));
const responses = [];
const portable = path => path.replace(/\\+/g, '/');
try {
  const args = process.argv.slice(2).map(argument => {
    if (!argument.startsWith('@')) return portable(argument);
    const out = join(scratch, `${responses.length}.rsp`);
    writeFileSync(out, portable(readFileSync(argument.slice(1), 'utf8')));
    responses.push(out);
    return `@${portable(out)}`;
  });
  if (args[0] !== '-flavor') args.unshift('-flavor', 'darwin');
  const result = spawnSync(lld, args, { stdio: 'inherit' });
  if (result.error) console.error(result.error.message);
  process.exitCode = result.status ?? 1;
} finally {
  for (const out of responses) unlinkSync(out);
  rmdirSync(scratch);
}
