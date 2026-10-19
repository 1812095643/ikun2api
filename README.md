# IKUN2api

**版本 1.0.0** · AI 账号与请求网关

提供 Claude、OpenAI / Codex、Gemini、Grok 等平台的账号、代理、分组、额度、用量与请求管理，包含 Vue 管理界面和 Go 服务端。

本仓库同时提供客户端协议采样工具和独立远程浏览器配置。采样、浏览器运行环境和网关传输各自记录，避免混用不同入口的身份数据。

## 开始使用

需要 Docker 与 Docker Compose，或者 Go 1.27、Node.js 24 与 pnpm 9。

```sh
git clone https://github.com/1812095643/ikun2api.git
cd ikun2api
cp deploy/.env.example deploy/.env
chmod 600 deploy/.env
```

填写 `deploy/.env` 中的数据库、Redis 密码和管理员信息，然后从源码构建：

```sh
docker compose -f deploy/docker-compose.local.yml up -d --build
```

默认访问端口为 `8080`。数据库、Redis、账号和密钥应使用持久化目录。已有部署升级请先参考[部署与升级](docs/DEPLOYMENT.md)。

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
