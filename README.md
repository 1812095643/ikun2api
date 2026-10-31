# IKUN2api

**版本 1.3.20** · AI 账号与请求网关

提供 Claude、OpenAI / Codex、Gemini、Grok 等平台的账号、代理、分组、额度、用量与请求管理，包含 Vue 管理界面和 Go 服务端。

本仓库同时提供客户端协议采样工具和独立远程浏览器配置。采样、浏览器运行环境和网关传输各自记录，避免混用不同入口的身份数据。

## 从 Sub2API 带数据切换

在原服务器执行；Compose 部署先进入原部署目录：

```bash
curl -fsSL https://raw.githubusercontent.com/1812095643/ikun2api/main/deploy/migrate.sh | sudo bash
```

自动校验、备份、迁移和核对数据，保留账号、余额、Key、订阅、用量、数据卷及加密配置。支持 Linux systemd 与 Docker Compose 单实例部署。详见 [迁入与回退](deploy/MIGRATION.md)。

## 后续更新

管理员点击左上角版本号 → **检查更新** → **备份并一键更新**。只获取 IKUN 正式签名发布包，无需合并源码。

[发布与下载](https://github.com/1812095643/ikun2api/releases/latest) · [新安装](deploy/README.md) · [维护者发布流程](scripts/release/README.md)

| 内容 | 入口 |
| --- | --- |
| 客户端头与 TLS 的获取、核对、修改方法 | [客户端协议采样](docs/CLIENT_PROFILE.md) |
| Firefox、noVNC、代理与防直连配置 | [远程浏览器](docs/REMOTE_BROWSER.md) |
| 遥测半成品的范围与暂停状态 | [遥测状态](docs/TELEMETRY_STATUS.md) |
| 源码构建、服务配置与升级 | [部署说明](docs/DEPLOYMENT.md) |
| 本版改动及验证边界 | [更新记录](docs/CHANGELOG.md) |

## 当前边界

- 遥测处于实验状态，默认关闭，开发暂停；没有完成真实 OAuth 会话验收。
- 客户端采样使用临时 HOME 和本地测试凭据，不导出真实账号会话。
- 本地 HTTP / TLS 采样、配置核对与离线回归均不能证明账号不会被封禁。
- 保留现有数据库结构、插件协议及兼容配置键，不因品牌变更替换账号身份或密钥。

## 许可

采用 [GNU LGPL v3.0 或更高版本](LICENSE)，并附带其引用的 [GNU GPL v3](COPYING.GPL)。版权与修改声明见 [NOTICE](NOTICE.md)。
