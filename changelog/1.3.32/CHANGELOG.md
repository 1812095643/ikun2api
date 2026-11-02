# IKUN2api 1.3.32

- 补齐 Sub2API 0.2.19 历史模型与元数据：Haiku 5.5、Gemini 3.6 Flash 默认价格、Codex 模型能力及缺省窗口。
- 修复 Chat / Responses 的 developer 角色、具名工具选择、旧函数调用配对、拒绝响应和思考块字段兼容。
- 修复 Claude 缓存 TTL 顺序、锁定采样参数和工具变更 beta；保留 IKUN 的隐私过滤、受控指纹、遥测与客户端准入。
- 修复 WS 生图 token 统计、GPT-6.1 Sol OAuth 长上下文附加费豁免，以及长连接后续轮次的分组调价生效。
- 补齐 Gemini 工具结果图片与第三方上游 429 处理；接入统一协议/平台目录、Command Code/Cline、多语言分组和组合路由模型目录。
- 同步官方 main 的 OAuth 非流式 JSON、incomplete/cancelled 终态、无 Content-Type SSE 检测及前端依赖补丁。
- 保留 BPS、连接亲和与搜索/生图续接、原生页面、导航和已有 SQL 校验和；管理员自定义价格与历史账单不作重写。

本版沿用现有备份和签名校验的一键更新流程，无需重建数据库或重新导入账号。管理 MCP 未纳入本轮。
