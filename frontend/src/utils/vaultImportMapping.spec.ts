import { describe, it, expect } from 'vitest'
import { mapVaultColumns } from './vaultImportMapping'
describe('vault custom column mapping', () => {
 it('supports reordered columns and keeps secret whitespace', () => {
  const out = JSON.parse(mapVaultColumns('sk-ant-sid02-TEST----a@example.test----  pass  ----aux@example.test----auxpass', ['session_key', 'email', 'password', 'auxiliary_email', 'auxiliary_password'], '----'))
  expect(out[0].password).toBe('  pass  '); expect(out[0].email).toBe('a@example.test')
 })
 it('rejects ambiguous delimiters, duplicate mapping and missing email', () => {
  expect(() => mapVaultColumns('a----b----c', ['email', 'password'], '----')).toThrow('列数')
  expect(() => mapVaultColumns('a----b', ['email', 'email'], '----')).toThrow('同一个字段')
  expect(() => mapVaultColumns('a', ['password'], '----')).toThrow('主邮箱')
 })
})
