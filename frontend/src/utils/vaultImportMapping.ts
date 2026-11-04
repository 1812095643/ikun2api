import type { VaultEntry } from '@/api/accountVault'
export function mapVaultColumns(raw: string, fields: string[], delimiter: string): string {
 if (!fields.includes('email')) throw new Error('列映射必须包含主邮箱。')
 const used = fields.filter(Boolean)
 if (new Set(used).size !== used.length) throw new Error('同一个字段不能映射到多列。')
 const rows = raw.replace(/\r\n/g, '\n').split('\n').filter(line => line.trim()).map((line, index) => {
  const cells = line.split(delimiter)
  if (cells.length !== fields.length) throw new Error('第 ' + (index + 1) + ' 行列数与映射不一致，请检查分隔符。')
  const entry: Partial<VaultEntry> = {}
  fields.forEach((field, i) => { if (field) Object.assign(entry, { [field]: cells[i] }) })
  return entry
 })
 return JSON.stringify(rows)
}
