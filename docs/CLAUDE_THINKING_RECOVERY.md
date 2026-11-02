# Claude thinking 签名恢复

## 来源与确认范围

2026-11-02 核对官方 npm 的 `@anthropic-ai/claude-code`、`@anthropic-ai/claude-code-win32-x64`，版本均为 2.1.316。原生包通过 npm integrity 校验；从原包提取的程序与分析样本 SHA-256 一致：`e896a9d8da519353a49bf69ca4a695ba7e09067752687c675b3969041e632d52`。

分析的是正式程序内嵌的 JavaScript，不把 GitHub 的说明仓库称为完整 CLI 源码。原始程序、提取脚本和派生源码仅保留在本地审计产物，不进入公开发布源码。

本次 108 的 36 条上游 400 全部是历史 `thinking.signature` 无效；客户端最终均获得模型输出。它们集中于两个会话的固定历史位置。没有原始入站/出站快照，不能据错误摘要断言签名最初由客户端或网关破坏，也没有 TLS、UA 或设备身份导致该错误的证据。

## 官方行为

- CLI 和其内嵌 SDK 对 `signature_delta` 保存上游给出的完整签名值；thinking 文本增量单独拼接。网关应原样转发签名事件，不合并、不重算。
- 明确的 thinking 签名 400 会清理当时的旧 thinking/redacted_thinking，再重试。工具调用、工具结果和普通正文保留；只剩思考块的消息使用占位文本维持非空结构。
- CLI 把 `thinking_stripped` 标记写入本地会话历史；重放时清理标记之前的思考块，新生成的思考块继续保留。
- 已提取相关纯函数，用合成会话验证旧历史清理、工具配对、新历史保留和原始对象不变。该验证不是对真实付费会话的重放。

## IKUN2api 的适配

API 网关不能改写 Desktop/CLI 的本地会话文件，因此用有界摘要缓存表达已经恢复成功的历史：

1. 仅原生 Anthropic OAuth/setup-token、Anthropic strict 模型族、管理员启用签名整流时使用。
2. 仅对明确的 `Invalid signature in thinking block` HTTP 400 使用官方式历史清理。其他错误及第三方协议保留既有处理路径。
3. 清理旧块时保持当前请求的 thinking 模式、工具定义、工具配对、普通内容与大整数精度。失败的思考正文不会转成普通 assistant 文本再次发送。
4. 只有清理后的请求被上游接受，才记住已移除块的规范化 SHA-256 摘要；失败或取消不产生成功恢复记录。
5. 摘要按已鉴权用户、API Key、分组、账号及其账号身份/自定义地址、实际模型、原始客户端会话信号隔离。缺少可信 Key 或会话信号时仅修复本次请求。
6. 后续请求只移除摘要完全匹配的旧块。内容、签名或其他字段改变便不匹配；新生成的思考块不受影响。
7. 缓存只在当前进程内保存，1 小时过期，全局最多 4096 项，每次最多记住 256 个块。重启、过期或容量淘汰后可能重新发生一次兼容重试；多实例分别学习，不宣称永久消除所有签名错误。

缓存不包含思考正文、原始签名、用户输入、凭据或完整会话内容。签名错误诊断只记录账号编号、错误位置和该块是否匹配 Forward 入口摘要；匹配证明该块经过 Forward 时未变，不匹配本身不能断言被篡改。

## 合并与验证约束

重点保留 `claude_thinking_recovery.go`、`GatewayService` 的有界缓存，以及 `gateway_forward.go` 的入口摘要、发送前历史清理、明确签名错误分支、成功后的记忆。保留原有 403 策略、慢首发整流预算、取消语义、其他供应商的 passback-required/unknown 行为。

回归至少覆盖首轮 400→恢复、下一轮单次成功、新 thinking 保留、SSE 签名事件原样且有序、失败不记忆、开关关闭、用户/Key/账号/模型/会话隔离、过期、容量上限和并发访问。不要用结构检查冒充密码学验签，也不要用本地模拟成功代替真实线上归因。

官方参考：

- https://registry.npmjs.org/@anthropic-ai/claude-code/latest
- https://registry.npmjs.org/@anthropic-ai/claude-code-win32-x64/latest
- https://platform.claude.com/docs/en/build-with-claude/thinking-troubleshooting
