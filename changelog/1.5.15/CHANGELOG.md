# IKUN2api 1.5.15

- 修复 Claude 账号白名单未进入调度缓存的问题，统一 Messages、Chat Completions 和 Responses 的转发检查。
- GPT WebSocket 每轮检查当前账号的模型资格，账号移除模型后立即生效。已配置的透传白名单继续参与选号。
- 池模式在上游明确回报模型不可用后，仅暂时停止该账号对该模型的调度，保留其余模型可用。
- 同步 Sub2API 0.2.24，包含网关请求校验、Codex 搜索 403 误停账号、OpenAI OAuth 令牌轮换和流式响应修复。
