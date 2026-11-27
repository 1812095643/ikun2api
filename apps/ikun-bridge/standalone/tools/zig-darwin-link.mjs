import { spawnSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { join } from 'node:path';

const [arch, ...args] = process.argv.slice(2);
if (!['aarch64', 'x86_64'].includes(arch)) process.exit(2);
const sdk = process.env.SDKROOT;
if (!sdk || !existsSync(join(sdk, 'usr/lib/libSystem.tbd'))) throw new Error('Set SDKROOT to an extracted macOS SDK');
const result = spawnSync('zig.exe', ['cc', `--target=${arch}-macos.11.0`, '--sysroot', sdk, ...args.filter(arg => !arg.startsWith('--target='))], { stdio: 'inherit' });
if (result.error) console.error(result.error.message);
process.exitCode = result.status ?? 1;
