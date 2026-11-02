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

## 1.3.34：从响应环节防止生成坏历史

进一步对照完整流转链，确认旧的 `restoreToolNamesInBytes` 对整段响应执行字符串替换。工具别名出现在 thinking 正文时会被一并还原，签名却保持原值；别名也可能误改参数、ID 或加密内容。这不是需要等待上游失败才能处理的问题。

修复后只在已知 Anthropic、Responses、Chat Completions 工具名字段做完整名称查找。保留既有动态/静态映射、请求侧工具名改写、账号身份与会话隔离，但不触碰任何 thinking、signature、redacted_thinking、encrypted_content、正文、参数或 ID。完整 JSON、SSE 单行/多行 data 与 CRLF 均按原始字节位置替换名称，未改字段保持原始编码。

两轮合成上游回放在旧代码上复现“第一轮修改 thinking → 第二轮签名失败 → 第三次请求兼容重试”；修复后两轮只发两次请求。开启账户身份重映射、会话 masking 和隐私清理后的测试同样通过。这里的合成签名只验证完整性约束，不能冒充 Anthropic 密码学验签或证明原 36 条 Desktop 记录均由此造成；原 Desktop 请求是否进入工具改写分支没有留存证据。

另增加独立来源摘要缓存：记录上游已返回的签名摘要和思考内容摘要，按用户/Key/账号/模型/原始会话隔离，最多 4096 项、1 小时。SSE 只在 block stop 后记完整块，signature_delta 采用官方完整值赋值语义；没有收齐的块不入缓存。该缓存只诊断，不触发清理或改变任何转发策略。

新签名错误的日志可区分：

- `matches_previous_upstream_response`：与此范围内此前观察到的上游内容一致仍被拒绝，需要继续检查上下文/身份绑定。
- `changed_before_request_forwarding`：同一签名对应的内容在下一轮进入 Forward 前已经变化；不能仅据此归咎客户端，前置转换也需核对。
- `changed_during_request_forwarding`：同一签名的内容在 Forward 入口与实际出站之间发生变化。
- `not_observed_in_this_scope` / `unavailable`：缺少历史证据，可能是旧会话、切换范围、重启、过期或容量淘汰，不推定客户端错误。

日志另带本次 `mimic_body`、`tool_name_rewrite` 布尔值，确认分支；不输出签名、思考内容、身份明文或原始摘要。来源判断只比较已观察到的数据，不保证用户完整收到所有输出，也不据此认定官方故障。

## 合并与验证约束

### 1.3.35：保留普通回答工具名显示的可逆还原

1.3.34 将还原限制在协议工具名，保护了签名，但同时收窄了普通回答显示真名的行为。1.3.35 将普通助手文字的显示还原独立出来：输出把已知工具别名改回真名，输入只对曾经输出、完整摘要匹配且未出现歧义的 assistant 历史文字应用原位置的逆向补丁。原文里原本存在的同名真实工具不会被一并改成别名，用户消息、参数、ID 和 signed thinking 不参与这种恢复。

实现仅保存文字摘要、替换位置与工具名，不保存完整助手正文。记录按鉴权用户、Key、分组、账号、实际模型和客户端会话隔离；没有显式会话时，使用首条用户内容摘要作为调用方提供的稳定线索，同 Key 内完全相同的开头无法证明属于不同会话，因此歧义拒绝不能被删去。来源不唯一、文字被编辑或证据过期时不猜测逆向替换。

缓存为进程内有界结构，最多 2048 项、约 8 MiB、24 小时有效；单段最多 256 处替换，超过限制的后续部分保留原样，同时保留已有替换的逆向记录。重启、不同实例、容量淘汰、合并/拆分历史文本都可能导致无法命中；仍由既有明确错误后的整流兜底，不能声称所有跨轮报错或账号风险已消除。

流式处理按普通文本片段替换，语义与旧版按 chunk 还原一致；不将 thinking/signature 或工具参数拼进普通文字。SSE 多行 data、CRLF、普通最终响应、Chat/Responses 文本字段均有回归。工具别名跨多个文本片段拆开时可能保留别名，不能以合并签名/任意参数等方式弥补。

新增 `claude_tool_text_roundtrip.go`、`TestClaudeToolText*`、`TestForwardClaudeToolText*`。同步时应一起保留显示还原和输入精确逆向恢复，不能只合入一边，也不能恢复整段 ReplaceAll。该改动针对可验证的功能和协议完整性，不是关于供应商检测或账号封禁的保证。

重点保留 `claude_thinking_recovery.go`、`GatewayService` 的有界缓存，以及 `gateway_forward.go` 的入口摘要、发送前历史清理、明确签名错误分支、成功后的记忆。保留原有 403 策略、慢首发整流预算、取消语义、其他供应商的 passback-required/unknown 行为。

回归至少覆盖首轮 400→恢复、下一轮单次成功、新 thinking 保留、SSE 签名事件原样且有序、失败不记忆、开关关闭、用户/Key/账号/模型/会话隔离、过期、容量上限和并发访问。不要用结构检查冒充密码学验签，也不要用本地模拟成功代替真实线上归因。

响应链同时回归 `TestToolResponseIntegrity*`、`TestForwardToolResponseIntegrity*`、`TestClaudeThinkingTrace*`；禁止重新引入整段响应字符串替换。

官方参考：

- https://registry.npmjs.org/@anthropic-ai/claude-code/latest
- https://registry.npmjs.org/@anthropic-ai/claude-code-win32-x64/latest
- https://platform.claude.com/docs/en/build-with-claude/thinking-troubleshooting
