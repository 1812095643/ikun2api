# Sub2API 迁入 IKUN2api

迁入只替换应用程序，复用原 PostgreSQL、Redis、账号、余额、API Key、订阅、历史用量与数据目录。保留原 JWT、TOTP 和加密配置，不需要导出账号再重建。

## 一键迁入

在原服务器运行；Docker 部署请先进入原 Compose 目录：

```bash
curl -fsSL https://raw.githubusercontent.com/1812095643/ikun2api/main/deploy/migrate.sh | sudo bash
```

原 systemd 服务名默认为 `sub2api`；自定义名称可指定：

```bash
curl -fsSL https://raw.githubusercontent.com/1812095643/ikun2api/main/deploy/migrate.sh | sudo bash -s -- --mode systemd --service sub2api
```

指定原 Compose 文件和服务名：

```bash
curl -fsSL https://raw.githubusercontent.com/1812095643/ikun2api/main/deploy/migrate.sh | sudo bash -s -- --mode compose --compose-file ./docker-compose.yml --compose-service sub2api
```

可多次传入 `--compose-file`，顺序与原部署一致。`--check` 只下载校验并做只读兼容检查，不停服。`--version v1.3.16` 可固定版本。

## 切换顺序

1. 获取正式 GitHub Release，校验 Ed25519 签名、SHA-256 和程序版本。
2. 使用原运行环境读取原配置，只读核对已应用的迁移名称与校验和。
3. 停止原应用，备份完整数据库、数据目录和运行环境。不停止 PostgreSQL 或 Redis。
4. systemd 只新增独立 drop-in；Compose 只新增 `ikun2api.override.yml`。原服务文件、`.env`、数据库名和持久化卷不改名。
5. 在维护状态启动 IKUN，执行增量迁移、健康检查与业务数据摘要比对，通过后恢复访问。
6. 失败时恢复本次数据库快照，撤回本次应用覆盖配置并启动原程序。脚本不执行 `down -v`、删除数据库或重建密钥。

## 备份位置

- systemd：`/opt/ikun2api-switch-backups/<UTC时间>/`。
- Compose：原应用数据卷内 `/app/data/ikun-switch-backups/<UTC时间>/`。
- 后续网页更新：运行程序旁的 `upgrade-backups/`。

备份包含账号及密钥，目录权限为 0700、文件为 0600，请勿公开上传。

## 后续更新

管理员点击左上角版本号 → **检查更新** → **备份并一键更新**。系统只安装本仓库签名的正式发布包，完成兼容检查、备份和程序替换后自动重启，无需克隆源码或合并代码。

Compose 的运行程序保存在原数据卷的 `runtime/` 下，同一镜像的容器重启或重建会保留网页更新后的程序。后续手动操作 Compose 时须带上覆盖文件：

```bash
docker compose -f docker-compose.yml -f ikun2api.override.yml up -d
```

镜像注册表不可用时，迁入脚本自动获取同一 GitHub Release 的签名离线镜像包，不需要另配注册表账号。

## 兼容边界

当前基于官方 Sub2API 0.2.19 数据结构，保留已合入的历史迁移和官方历史校验和兼容规则。未知二开迁移、更新版数据结构、校验和冲突、多实例共享数据库、特殊启动参数或多网络部署会停止自动迁入，需要单独核对。不要绕过预检强制切换。

迁移是向前执行的。运行 IKUN 一段时间后，不能只换回旧程序就当作完整数据回退；需评估后来产生的数据，选择兼容版本或明确恢复时间点。
