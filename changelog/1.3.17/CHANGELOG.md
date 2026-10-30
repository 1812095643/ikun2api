# IKUN2api 1.3.17

- 修复从 Windows 历史构建迁入时，`259_claude_device_identities.sql` 的 CRLF/LF 校验差异。只兼容已核实的两个确切摘要，不修改已应用的迁移记录或设备 ID。

- 发布完整源码与 Linux、macOS、Windows 产物，提供 amd64/arm64 容器及离线镜像包。
- 新增 Sub2API 带数据迁入脚本：兼容预检、完整备份、增量迁移、健康检查和数据摘要核对。
- 恢复管理员版本按钮与一键更新，连接 IKUN GitHub Release，使用者无需手动合并代码。
- 强制校验 Ed25519 发布签名与 SHA-256；备份失败或数据结构不兼容时停止替换。
- 保留官方 0.2.19 修复、IKUN 协议与连接优化、隐私过滤、原生页面、工单、签到抽奖、上游余额和邮件找回密码。

[迁入与回退说明](https://github.com/1812095643/ikun2api/blob/main/deploy/MIGRATION.md)

```bash
curl -fsSL https://raw.githubusercontent.com/1812095643/ikun2api/main/deploy/migrate.sh | sudo bash
```
