import { readFile, writeFile } from 'node:fs/promises';

const [output, ...input] = process.argv.slice(2);
if (!output || input.length !== 2) throw new Error('usage: universal.mjs OUTPUT ARM64 X64');
const binaries = await Promise.all(input.map(path => readFile(path)));
const header = Buffer.alloc(8 + 20 * binaries.length);
const page = 1 << 14;
header.writeUInt32BE(0xcafebabe, 0);
header.writeUInt32BE(binaries.length, 4);
let offset = header.length;
const parts = [header];
for (const [index, binary] of binaries.entries()) {
  if (binary.readUInt32LE(0) !== 0xfeedfacf || binary.readUInt32LE(4) !== [0x0100000c, 0x01000007][index]
      || binary.readUInt32LE(12) !== 2) throw new Error(`Not an expected executable: ${input[index]}`);
  const aligned = Math.ceil(offset / page) * page;
  const pos = 8 + index * 20;
  header.writeUInt32BE(binary.readUInt32LE(4), pos);
  header.writeUInt32BE(binary.readUInt32LE(8), pos + 4);
  header.writeUInt32BE(aligned, pos + 8);
  header.writeUInt32BE(binary.length, pos + 12);
  header.writeUInt32BE(14, pos + 16);
  parts.push(Buffer.alloc(aligned - offset), binary);
  offset = aligned + binary.length;
}
await writeFile(output, Buffer.concat(parts));
