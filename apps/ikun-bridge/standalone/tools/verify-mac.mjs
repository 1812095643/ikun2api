import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import assert from 'node:assert/strict';

const contents = join(process.argv[2], 'Contents');
const executable = readFileSync(join(contents, 'MacOS', 'IKUNBridge'));
const hash = bytes => createHash('sha256').update(bytes).digest();
const plistHash = hash(readFileSync(join(contents, 'Info.plist')));
const resourcesHash = hash(readFileSync(join(contents, '_CodeSignature', 'CodeResources')));
assert.equal(executable.readUInt32BE(0), 0xcafebabe);
assert.equal(executable.readUInt32BE(4), 2);
const archNames = new Map([[0x0100000c, 'arm64'], [0x01000007, 'x86_64']]);
const results = [];
for (let index = 0; index < 2; index++) {
  const record = 8 + index * 20;
  const start = executable.readUInt32BE(record + 8);
  const end = start + executable.readUInt32BE(record + 12);
  const align = 2 ** executable.readUInt32BE(record + 16);
  assert(start % align === 0 && end <= executable.length);
  const slice = executable.subarray(start, end);
  assert.equal(slice.readUInt32LE(0), 0xfeedfacf);
  const cpu = slice.readUInt32LE(4);
  assert.equal(cpu, executable.readUInt32BE(record));
  const arch = archNames.get(cpu);
  assert(arch);
  assert.equal(slice.readUInt32LE(12), 2);
  let pos = 32, signatureStart = 0, signatureSize = 0, deployment = 0;
  const libraries = [];
  for (let command = 0; command < slice.readUInt32LE(16); command++) {
    const kind = slice.readUInt32LE(pos);
    const size = slice.readUInt32LE(pos + 4);
    assert(size >= 8 && pos + size <= slice.length);
    if (kind === 0x1d) [signatureStart, signatureSize] = [slice.readUInt32LE(pos + 8), slice.readUInt32LE(pos + 12)];
    if (kind === 0x32) {
      assert.equal(slice.readUInt32LE(pos + 8), 1);
      deployment = slice.readUInt32LE(pos + 12);
    }
    if ([0xc, 0x80000018, 0x8000001f, 0x80000023].includes(kind)) {
      libraries.push(slice.toString('utf8', pos + slice.readUInt32LE(pos + 8), pos + size).split('\0')[0]);
    }
    pos += size;
  }
  assert.equal(deployment, 0xb0000);
  assert(libraries.some(path => path.includes('/WebKit.framework/')));
  assert(libraries.every(path => path.startsWith('/usr/lib/') || path.startsWith('/System/Library/')));
  assert(signatureStart > pos && signatureStart + signatureSize <= slice.length);
  const signature = slice.subarray(signatureStart, signatureStart + signatureSize);
  assert.equal(signature.readUInt32BE(0), 0xfade0cc0);
  let directory;
  for (let slot = 0; slot < signature.readUInt32BE(8); slot++) {
    if (signature.readUInt32BE(12 + slot * 8) !== 0) continue;
    const offset = signature.readUInt32BE(16 + slot * 8);
    directory = signature.subarray(offset, offset + signature.readUInt32BE(offset + 4));
  }
  assert(directory);
  assert.equal(directory.readUInt32BE(0), 0xfade0c02);
  assert(directory.readUInt32BE(12) & 2, 'Expected a local ad-hoc signature');
  const hashOffset = directory.readUInt32BE(16);
  const pages = directory.readUInt32BE(28);
  const limit = directory.readUInt32BE(32);
  const width = directory[36];
  const pageSize = 2 ** directory[39];
  assert.equal(limit, signatureStart);
  assert.equal(directory[37], 2);
  assert.equal(width, 32);
  assert.equal(pages, Math.ceil(limit / pageSize));
  const slotHash = slot => directory.subarray(hashOffset + slot * width, hashOffset + (slot + 1) * width);
  assert(slotHash(-1).equals(plistHash), 'Info.plist digest differs');
  assert(slotHash(-3).equals(resourcesHash), 'CodeResources digest differs');
  for (let slot = 0; slot < pages; slot++) {
    const digest = hash(slice.subarray(slot * pageSize, Math.min((slot + 1) * pageSize, limit)));
    assert(slotHash(slot).equals(digest), `${arch} code page ${slot} digest differs`);
  }
  results.push({ arch, pages, minMacOS: '11.0', webkit: true, adHoc: true });
}
assert.equal(new Set(results.map(result => result.arch)).size, 2);
console.log(JSON.stringify(results));
