# IKUN2api 1.4.8

- 同步官方 Sub2API v0.2.20，保留 IKUN2api 的协议、隐私、计费和管理功能。
- 新增管理 MCP、OAuth/PKCE 授权、连接管理和管理员权限；MCP 默认关闭，配置 `MCP_PUBLIC_URL` 后启用。
- 新增邮箱黑名单、Gmail 身份归一化与批量禁用，完善管理员 API Key 权限。
- 新增 API Key 智能路由与有序备用分组，模型列表自动合并，支持 CC-Switch、Magpie 导入。
- 优化渠道状态卡片、历史时间线与筛选，沿用现有主题和可见分组权限。
- 同步 GPT Image 2.6、1K/2K/4K 生图分流、Terra 模型元数据及计费修复。
- 修复 Chat/Responses、WebSocket、Claude、Gemini、Antigravity 和 Grok 兼容问题，完善请求内存预算、读写超时及任务治理。
- 完善中文与 emoji 的 JavaScript 字符处理；HTTP/WebSocket 正文保持 UTF-8 JSON。

升级会自动执行新增数据库迁移，保留现有账号、密钥、余额和配置。请保留更新器生成的备份；数据库迁移为向前执行。
