# IKUN2api 1.3.37

- API 密钥的“导入到 CCS”统一打开配置弹窗，可选择客户端、接口格式和模型。
- 对齐 CC Switch v4.0.25，新增 OpenCode、Pi、OpenClaw、Hermes、Antigravity CLI 等目标，保留 Claude Code、Codex、Gemini CLI 和 Grok Build。
- Claude Desktop 使用 CCS 官方支持的两步迁入流程，明确提示暂不支持直接深链接。
- 模型从当前 Key 的目录读取，可刷新、搜索或手动输入；移除过时的固定默认模型。
- 按客户端 SDK 规范处理 /v1、/v1beta 与 Antigravity 前缀，保留 Codex 模型目录及 API Key 生图配置。
- 完善余额查询、非必要遥测选项、分组客户端限制、Key 切换取消与动态配置输入检查。
- 移除 100 毫秒焦点检测导致的“CCS 未安装”误报，增加复制导入链接。
- 更新新手教程与发布回归。

本次不修改数据库结构、上游账号、推理协议、指纹或额度。
