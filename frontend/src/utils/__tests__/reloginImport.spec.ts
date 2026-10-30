import { describe, expect, it } from 'vitest'
import { parseReloginImport } from '../reloginImport'

describe('粘贴登录资料', () => {
  it('整行资料保留密码特殊字符，兼容空行和换行', () => {
    expect(parseReloginImport('\nUSER@example.test----p^a_$&!ss----ABCDEF\r\n\r\nsecond@example.test----pass\n'))
      .toEqual([
        { email: 'user@example.test', password: 'p^a_$&!ss', mfa_secret: 'ABCDEF', auth_mode: 'password_2fa' },
        { email: 'second@example.test', password: 'pass', mfa_secret: undefined, auth_mode: 'password_2fa' }
      ])
  })
  it.each(['single', 'array', 'export'])('兼容 %s JSON 资料并只提取凭据字段', format => {
    const item = { email: 'user@example.test', password: ' pass ', mfa_code: '123456', account_id: 999, apply: false }
    const value = format === 'single' ? item : format === 'array' ? [item] : { items: [item] }
    expect(parseReloginImport(JSON.stringify(value))).toEqual([
      { email: item.email, password: item.password, mfa_code: item.mfa_code, auth_mode: 'password_2fa' }
    ])
  })
  it('保留邮箱验证码方式和收件箱', () => {
    expect(parseReloginImport(JSON.stringify({ email: 'user@example.test', auth_mode: 'email_otp', inbox_url: 'https://example.test/inbox' })))
      .toEqual([{ email: 'user@example.test', auth_mode: 'email_otp', inbox_url: 'https://example.test/inbox' }])
  })
  it.each([
    ['', '请先粘贴'],
    ['user@example.test', '第 1 行'],
    ['user@example.test----pass----KEY----EXTRA', '第 1 行'],
    ['user@example.test--------KEY', '需要填写密码'],
    ['no-email----pass', '完整邮箱'],
    ['user@example.test----pass\nUSER@example.test----other', '邮箱重复'],
    ['null', '第 1 行'],
    ['[null]', '格式不完整'],
    ['{"items":{}}', '1 至 100'],
    ['{"email":"user@example.test","password":123}', '格式或长度'],
    ['{"email":"user@example.test","auth_mode":"other"}', '验证方式']
  ])('拒绝不完整或重复资料：%s', (input, message) => {
    if (message === '验证方式') {
      expect(() => parseReloginImport(input)).toThrow('密码验证或邮箱验证码')
    } else expect(() => parseReloginImport(input)).toThrow(message)
  })
  it('JSON 错误提示不包含原始密码片段', () => {
    const sensitive = 'private-example-password'
    expect(() => parseReloginImport(`{"password":${sensitive}}`)).toThrow('JSON 格式不完整')
    try { parseReloginImport(`{"password":${sensitive}}`) } catch (error) {
      expect(String(error)).not.toContain(sensitive)
    }
  })
  it('超过批量上限或资料大小时停止解析', () => {
    const item = { email: 'user@example.test', password: 'pass' }
    expect(() => parseReloginImport(JSON.stringify(Array.from({ length: 101 }, () => item)))).toThrow('1 至 100')
    expect(() => parseReloginImport('x'.repeat(2 * 1024 * 1024 + 1))).toThrow('2 MB')
  })
  it('针对已有账号时核对邮箱并限制单条', () => {
    expect(parseReloginImport('USER@example.test----pass', 'user@example.test')).toHaveLength(1)
    expect(() => parseReloginImport('other@example.test----pass', 'user@example.test')).toThrow('邮箱与当前账号不一致')
    expect(() => parseReloginImport('user@example.test----pass\nother@example.test----pass', 'user@example.test')).toThrow('只粘贴该账号的一条')
  })
})
