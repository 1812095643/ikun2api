# CC Switch 导入维护记录

## 对照基线

2026-11-03 核实最新正式版本 v4.0.25，发布时间 2026-11-02T15:08:52Z（北京时间 2026-11-02 23:08:52）。标签指向 a3eafa408ae679db2e3401fca767ed2b89d9a2b2。主分支可能继续变化，本次以正式标签源码为准。

主要证据：
- https://github.com/farion1231/cc-switch/blob/v4.0.25/docs/guides/deeplink-site-integration-guide-zh.md
- https://github.com/farion1231/cc-switch/blob/v4.0.25/src-tauri/src/deeplink/parser.rs
- https://github.com/farion1231/cc-switch/blob/v4.0.25/src-tauri/src/deeplink/provider.rs
- https://github.com/farion1231/cc-switch/blob/v4.0.25/src-tauri/src/deeplink/codex_link.rs
- https://github.com/farion1231/cc-switch/blob/v4.0.25/docs/user-manual/zh/2-providers/2.6-claude-desktop.md

## 实现边界

- KeysView 只负责选中 Key；CcSwitchImportDialog 复用 BaseDialog、Select 和主题，负责显示表单；useCcSwitchImport 负责模型读取、取消和状态；ccswitchImport 负责独立配置生成及验证。
- 目标应用独立于分组厂商，按分组限制过滤客户端。TypeSafe 不生成错误的通用协议；OpenAI BPS 保留 Codex 路径。
- 原生 Claude 可不固定模型。其他客户端从当前 Key 的目录选择，只有唯一模型或有效的分组默认模型才自动选中；不硬编码某个发布时的“最新模型”。目录失败允许手动填写。
- Claude Desktop v4.0.25 的 parser 不接受直接供应商深链接。页面明确显示两步流程，先 app=claude，再由 CCS Desktop 页面迁入；不得改成不存在的 app 值或宣称已经直接导入 Desktop。
- OpenCode/Pi/OpenClaw/Hermes 的深链接目前只导入一个模型，完整模型属性不由该协议透传。导入后可在 CCS 扩展更多模型；本站不伪造未验证的上下文窗口、价格或能力。
- apiFormat 显式声明；OpenAI 格式使用单个 /v1，OpenCode 原生 Anthropic 使用 /v1、Gemini 使用 /v1beta；Pi/Hermes/Claude 使用对应 SDK 根地址。Antigravity 原生协议保留强制平台前缀。
- Codex 附带供应商 TOML、当前 Key、Responses、同源 model_catalog_url；沿用现有手动教程中的 API Key 生图扩展头。不写入 sandbox、MCP、notify 或无关用户全局配置。
- 使用 UTF-8 Base64 和 URLSearchParams；拒绝动态变量、文件引用、Pi 命令 Key、控制字符和 URL 内嵌凭据。
- 当前 Key 只用于配置及 GET /v1/models；不发起付费推理。请求不携带 Cookie，不跟随重定向，切换 Key/关闭弹窗会取消读取并丢弃旧响应。
- 余额查询复用 /v1/usage，单独传 gateway root 避免各 SDK 前缀污染；仅 HTTPS 自动启用。保留套餐名称、总额度、已用额度和负值不限量语义。
- 不以焦点或 100ms 定时判断是否安装。唤起后提示在 CCS 确认，提供复制链接；链接含 Key，不保存到 localStorage 或日志。

## 回归

针对性测试覆盖应用/格式/地址、Codex 配置、Desktop 路径、受限分组、异步 Key 隔离、目录异常、动态值拒绝及余额提取。发布流程纳入四套 CCS 回归。单测和网页验证不等同于各桌面客户端真实付费调用成功。
