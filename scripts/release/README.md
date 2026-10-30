# 维护者发布

1. 从 main 开发，保留同步规范规定的 IKUN 二开边界。
2. 修改 `backend/cmd/server/VERSION`、`frontend/package.json`，补充 `changelog/<版本>/CHANGELOG.md`。
3. 运行更新、迁移、认证与现有功能回归。
4. 提交并推送 main，创建匹配的 `vX.Y.Z` 标签并推送。
5. GitHub Actions 自动构建前端、五个平台二进制、双架构容器与完整源码；用仓库 Secret `IKUN_RELEASE_SIGNING_KEY` 签名后发布 Release。

```bash
git tag -a v1.3.17 -m "IKUN2api 1.3.17"
git push origin main v1.3.17
```

可先手动运行“发布 IKUN2api”，保持 dry_run=true。它构建并验证全部产物，但不创建正式 Release 或更新 latest 镜像。

签名私钥不能进入源码、日志、Release 或普通产物。客户端内置固定公钥；轮换须先发过渡版本，不能直接换钥导致旧版无法更新。

禁止修改已应用 SQL。每次涉及迁移的升级，必须用带数据的旧版本库演练，核对账号、余额、Key、订阅、历史记录和配置，并测试失败路径。不得通过覆盖迁移 checksum 放宽兼容校验。
