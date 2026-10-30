import type { LoginSecret } from '@/api/relogin'

export function parseReloginImport(input: string, accountEmail?: string): LoginSecret[] {
  const raw = input.trim()
  if (!raw) throw new Error('请先粘贴账号资料。')
  if (new TextEncoder().encode(raw).length > 2 * 1024 * 1024)
    throw new Error('资料超过 2 MB，请拆分后导入。')

  let records: unknown[]
  if (raw.startsWith('{') || raw.startsWith('[')) {
    let value: unknown
    try {
      value = JSON.parse(raw)
    } catch {
      // 解析器的原始异常可能包含密码片段，不直接展示。
      throw new Error('JSON 格式不完整，请检查后重试。')
    }
    records = Array.isArray(value) ? value : value && typeof value === 'object' && 'items' in value
      ? (value as { items: unknown[] }).items : [value]
  } else {
    records = raw.split(/\r?\n/).filter(line => line.trim()).map((line, index) => {
      const parts = line.split('----')
      if (parts.length < 2 || parts.length > 3)
        throw new Error(`第 ${index + 1} 行请使用「邮箱----密码----2FA 密钥」，密钥可省略。`)
      return { email: parts[0].trim(), password: parts[1].trim(), mfa_secret: parts[2]?.trim() }
    })
  }
  if (!Array.isArray(records) || !records.length || records.length > 100)
    throw new Error('每次支持导入 1 至 100 条资料。')
  if (accountEmail && records.length !== 1)
    throw new Error('为指定账号授权时，请只粘贴该账号的一条资料。')

  const emails = new Set<string>()
  return records.map((record, index) => {
    const prefix = `第 ${index + 1} 条资料`
    if (!record || typeof record !== 'object' || Array.isArray(record))
      throw new Error(`${prefix}格式不完整。`)
    const item = record as Record<string, unknown>
    const email = typeof item.email === 'string' ? item.email.trim().toLowerCase() : ''
    if (!/^[^\s@<>]+@[^\s@<>]+$/.test(email) || email.length > 320)
      throw new Error(`${prefix}请填写完整邮箱。`)
    if (accountEmail && email !== accountEmail.trim().toLowerCase())
      throw new Error('粘贴资料的邮箱与当前账号不一致，请核对后再授权。')
    if (emails.has(email)) throw new Error(`${prefix}邮箱重复，请每个账号只保留一条。`)
    emails.add(email)
    const authMode = item.auth_mode || 'password_2fa'
    if (authMode !== 'password_2fa' && authMode !== 'email_otp')
      throw new Error(`${prefix}请选择密码验证或邮箱验证码。`)
    const secret: LoginSecret = { email, auth_mode: authMode }
    const limits = { password: 1024, mfa_secret: 512, mfa_code: 32, inbox_url: 2048 } as const
    for (const field of Object.keys(limits) as (keyof typeof limits)[]) {
      const value = item[field]
      if (value == null) continue
      if (typeof value !== 'string' || value.length > limits[field])
        throw new Error(`${prefix}的${field === 'password' ? '密码' : '验证资料'}格式或长度需要调整。`)
      secret[field] = value
    }
    if (authMode === 'password_2fa' && !secret.password?.trim())
      throw new Error(`${prefix}需要填写密码。`)
    return secret
  })
}
