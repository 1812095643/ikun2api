/**
 * 验证并规范化 URL
 * 默认只接受绝对 URL（以 http:// 或 https:// 开头），可按需允许相对路径
 * @param value 用户输入的 URL
 * @returns 规范化后的 URL，如果无效则返回空字符串
 */
type SanitizeOptions = {
  allowRelative?: boolean
  allowDataUrl?: boolean
}

export function sanitizeUrl(value: string, options: SanitizeOptions = {}): string {
  const trimmed = value.trim()
  if (!trimmed) {
    return ''
  }

  if (options.allowRelative && trimmed.startsWith('/') && !trimmed.startsWith('//')) {
    try {
      const local = new URL(trimmed, 'https://local.invalid')
      return local.origin === 'https://local.invalid' && !/[\\\r\n\t]/.test(trimmed) ? trimmed : ''
    } catch {
      return ''
    }
  }

  // 允许 data:image/ 开头的 data URL（仅限图片类型）
  if (options.allowDataUrl && trimmed.startsWith('data:image/')) {
    return trimmed
  }

  // 只接受绝对 URL，不使用 base URL 来避免相对路径被解析为当前域名
  // 检查是否以 http:// 或 https:// 开头
  if (!trimmed.match(/^https?:\/\//i)) {
    return ''
  }

  try {
    const parsed = new URL(trimmed)
    const protocol = parsed.protocol.toLowerCase()
    if (protocol !== 'http:' && protocol !== 'https:') {
      return ''
    }
    return parsed.toString()
  } catch {
    return ''
  }
}

/**
 * 计算当前安装对外提供的 API 根地址（不含 /v1），供教程与首页接入示例共用
 * @param configured 后台配置的 API 地址，可为空
 * @param origin 当前页面来源，配置为空或无效时使用
 * @returns 去掉末尾斜杠与 /v1 的根地址
 */
export function resolveApiBase(configured: string, origin: string): string {
  return (sanitizeUrl(configured || '') || origin).replace(/\/+$/, '').replace(/\/v1$/, '')
}
