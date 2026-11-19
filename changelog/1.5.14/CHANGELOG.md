# IKUN2api 1.5.14

- 修复 Anthropic API Key 账号在普通 Forward 和透传路径遇到 thinking 签名 400 时直接失败的问题。
- 对明确的 `Invalid signature in thinking block` 只在同一账号上清理旧 thinking 历史并重试一次，保留当前 thinking 模式、工具调用和普通内容。
- 兼容旧的请求整流配置：缺少原生 thinking 签名字段时默认启用，管理员显式关闭仍然有效。
- 保留非签名 400、第三方 passback 协议和关闭整流时的原有行为。
