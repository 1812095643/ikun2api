# 部署与升级

## 从源码构建

后端需要 Go 1.27。前端需要 Node.js 24 与 pnpm 9。

```sh
pnpm --dir frontend install --frozen-lockfile
pnpm --dir frontend run build
cd backend
CGO_ENABLED=0 go build -tags embed -trimpath -ldflags="-s -w -X main.Version=1.0.0 -X main.BuildType=release" -o ikun2api ./cmd/server
./ikun2api -version
```

前端构建写入 `backend/internal/web/dist`，需要先构建前端，再编译带 `embed` 标签的后端。`backend/cmd/server/VERSION` 也是 `1.0.0`，不传版本参数时采用该值。

也可以从根目录执行 Docker Compose；应用镜像使用本仓库的 Dockerfile 现场构建，不依赖尚未发布的镜像。

```sh
cp deploy/.env.example deploy/.env
chmod 600 deploy/.env
docker compose -f deploy/docker-compose.local.yml up -d --build
```

部署前填写数据库、Redis、管理员及 JWT / 加密配置。首次生成的密钥应持久保存，升级时继续使用。生产访问应由反向代理提供 HTTPS。

## 已有服务器升级

1. 备份当前程序、环境文件、数据库及自定义设置，并记录当前程序校验值。
2. 在新目录放置新程序与 `resources`，保留原数据库、Redis、环境文件和账号缓存。
3. 切换程序路径并重启应用，检查 `/health`、管理员登录、`/api/v1/admin/system/version` 和站点名称。若检查未通过，切回原目录与原环境。

已有安装可能继续使用原服务名、数据目录或数据库名。无需为改名迁移账号表、重建 Redis 或重新生成密钥。新安装可以使用 `deploy/ikun2api.service`；已有服务应依据实际运行方式修改程序路径。

管理端的站点名称优先读取持久化设置。源码默认名称变更后，已有实例仍需要将 `site_name` 改为 `IKUN2api`；单改网页标题不足以覆盖后台保存的值。

## 实验功能

遥测默认关闭。现有安装请保留：

```ini
SUB6_CLAUDE_TELEMETRY_ENABLED=false
```

该变量保留既有名称以兼容当前部署。暂停期间不启用上报，也不以真实付费账号执行自动采样。客户端版本设置与 IKUN2api 自身的 `1.0.0` 版本是两回事，改品牌不要求升级 Claude / Codex 客户端。
