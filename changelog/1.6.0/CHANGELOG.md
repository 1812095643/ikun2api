# IKUN2api 1.6.0

- Claude 账号白名单在调度和上游转发前生效，涵盖 Messages、Chat Completions、Responses 和计数接口。渠道和账号映射指向同一目标时，原有别名仍可使用。
- GPT WebSocket 每轮检查账号的当前白名单，账号移除模型后同一连接的下一轮即受限制。GPT 透传账号的显式模型过滤设置继续生效。
- GPT 和 Claude 的 API Key 池收到上游明确的模型不存在响应时，仅冷却相应账号的该模型，其他模型可继续调度。
- 同步 Sub2API 0.2.24，包含网关请求校验、Codex 搜索 403 误停账号、OpenAI OAuth 令牌轮换及流式响应修复。
