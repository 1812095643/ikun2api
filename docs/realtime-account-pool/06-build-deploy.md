# 构建与部署

## 1. 这台构建机的特殊性

`43.131.251.155` 上装的是 **docker 28.2.2 但只有 legacy builder**：

```bash
$ docker buildx
docker: unknown command: docker buildx

$ apt-get install docker-buildx-plugin
E: Unable to locate package docker-buildx-plugin

$ ls /usr/libexec/docker/cli-plugins/
docker-compose        # 只有这一个
```

因此仓库里的 `Dockerfile` **不能直接 build**，它用了两个 BuildKit 特性：

| 写法 | legacy builder 的报错 |
|---|---|
| `FROM --platform=${BUILDPLATFORM} ${NODE_IMAGE} AS frontend-builder` | `failed to parse platform : "" is an invalid OS component` |
| `RUN --mount=type=cache,id=pnpm,target=/pnpm/store ...` | `the --mount option requires BuildKit` |

## 2. `Dockerfile.localbuild` 的三处改动

这个文件**在版本库里**（`dev-gw` 分支包含），方便在任何 legacy builder 环境复现构建。

### 改动 1：去掉 `--platform`

```diff
-FROM --platform=${BUILDPLATFORM} ${NODE_IMAGE} AS frontend-builder
+FROM ${NODE_IMAGE} AS frontend-builder
```

### 改动 2：删掉所有 `--mount=type=cache`

```diff
-RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm install --frozen-lockfile
+RUN pnpm install --frozen-lockfile
```

代价：每次构建重新下载依赖，慢 2-3 分钟。没有 BuildKit 就只能这样。

### 改动 3（必须）：提高 Node 堆上限

```diff
 FROM ${NODE_IMAGE} AS frontend-builder
+ENV NODE_OPTIONS=--max-old-space-size=3072
```

**不加这个会死在 vite 阶段**：

```
<--- Last few GCs --->
FATAL ERROR: Ineffective mark-compacts near heap limit Allocation failed - JavaScript heap out of memory
node:internal/... 
pnpm run build exited with code 134
```

原因：这台机器 3.7G 内存 + 6G swap，Node 默认堆上限按物理内存算，装不下
`vue-tsc -b && vite build` 编译 1392 个模块的峰值。

## 3. 构建命令

必须**后台跑**，因为 ssh 会话断开会把前台进程一起杀掉：

```bash
TAG=1.3.39-pool7

setsid nohup bash -c "
  cd /root/dev/ikun2api &&
  docker build \
    --build-arg TARGETOS=linux \
    --build-arg TARGETARCH=amd64 \
    --build-arg VERSION=${TAG} \
    -f Dockerfile.localbuild \
    -t ghcr.io/1812095643/ikun2api:${TAG} \
    . > /tmp/build_${TAG}.log 2>&1
  echo \"EXIT=\$?\" >> /tmp/build_${TAG}.log
  touch /tmp/build_${TAG}.done
" < /dev/null > /dev/null 2>&1 &
```

三个 `--build-arg` 的作用：

| ARG | 用途 |
|---|---|
| `TARGETOS` / `TARGETARCH` | 后面 `go build` 阶段交叉编译用；不传会被写成空的 `-tags` |
| `VERSION` | 写进二进制的版本号，前端「检查更新」和 `/api/v1/settings/public` 都读它 |

### 等待与判定

```bash
# 轮询
while [ ! -f /tmp/build_${TAG}.done ]; do sleep 15; done
tail -20 /tmp/build_${TAG}.log     # 末行必须是 EXIT=0
docker images | grep ${TAG}        # 应有 ~160MB
```

耗时约 **10–13 分钟**（pnpm install 3-4 分钟 + vite 3-4 分钟 + go build 4-5 分钟）。

### 构建阶段速览

```
Step  1-7   基础镜像 / 时区 / 证书
Step  8-20  frontend-builder：pnpm install → vue-tsc → vite build（产物 dist/）
Step 21-33  backend-builder：go mod download → go build -tags embed
Step 34     go build -tags embed  ← 把 dist/ 嵌进二进制
Step 35-54  运行时镜像：copy 二进制 + 时区数据 + 非 root 用户
```

## 4. 部署切换

`/root/ikun2api/docker-compose.yml:27` 写的是：

```yaml
image: ghcr.io/1812095643/ikun2api:${IKUN_VERSION:-latest}
```

**所以改 compose 文件是无效的**，必须改 `.env`：

```bash
# 1. 先给当前版本打回滚 tag
CURRENT=$(grep '^IKUN_VERSION=' /root/ikun2api/.env | cut -d= -f2-)
docker tag ghcr.io/1812095643/ikun2api:${CURRENT} \
           ghcr.io/1812095643/ikun2api:rollback-${CURRENT#1.3.39-}

# 2. 改版本号（第 1 行）
sed -i '1s/^IKUN_VERSION=.*/IKUN_VERSION='"${TAG}"'/' /root/ikun2api/.env

# 3. 只重建 app 容器，不动 postgres / redis
cd /root/ikun2api
docker compose up -d --no-deps --force-recreate ikun2api

# 4. 等健康
sleep 20
docker ps --filter name=ikun2api --format '{{.Names}}\t{{.Status}}'
```

`--no-deps` 是关键：不加的话 compose 会顺带检查/重建 postgres 和 redis，
有数据风险且没必要。

### 验证切换成功

```bash
docker inspect -f '{{.Image}}' ikun2api
# 应等于 docker images --no-trunc | grep ${TAG} 的 ID

curl -s http://127.0.0.1:18088/health          # {"status":"ok"}
curl -s https://sub.breeai.top/health          # {"status":"ok"}
curl -s https://sub.breeai.top/api/v1/settings/public | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['version'])"
# 应输出 ${TAG}
```

## 5. 回滚

```bash
sed -i '1s/^IKUN_VERSION=.*/IKUN_VERSION=1.3.39-pool6/' /root/ikun2api/.env
cd /root/ikun2api
docker compose up -d --no-deps --force-recreate ikun2api
```

当前保留的回滚点：

| tag | image id |
|---|---|
| `rollback-pool1` | `f4e5a3ddf3f0` |
| `rollback-pool2` | `d9560f061adb` |
| `rollback-pool3` | `2c7d49a8cdf4` |
| `rollback-pool4` | `42eea3fac866` |
| `rollback-pool5` | `a695be2e14f2` |

## 6. 数据库迁移

只有一个迁移文件：`backend/migrations/263_usage_log_account_snapshot.sql`。

```sql
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS account_name_snapshot TEXT,
    ADD COLUMN IF NOT EXISTS account_platform_snapshot TEXT;
```

`ADD COLUMN ... IF NOT EXISTS` 且**不带默认值**，所以在 PostgreSQL 11+ 上是纯元数据操作，
**不重写表、不锁表**。几十万行也毫秒级完成。

执行方式（服务启动时自动跑迁移，或手工）：

```bash
PGP=$(grep '^POSTGRES_PASSWORD=' /root/ikun2api/.env | cut -d= -f2-)
docker exec -e PGPASSWORD="$PGP" ikun2api-postgres \
  psql -U ikun2api -d ikun2api -f /path/to/263_usage_log_account_snapshot.sql
```

验证：

```bash
docker exec -e PGPASSWORD="$PGP" ikun2api-postgres \
  psql -U ikun2api -d ikun2api -c "\d+ usage_logs" | grep snapshot
#  account_name_snapshot      | text |
#  account_platform_snapshot  | text |
```

## 7. 磁盘与清理

这台机器盘只有 59G，构建过程会留垃圾。经验值：

| 项 | 大小 | 处理 |
|---|---|---|
| 悬空镜像（构建失败残留） | 每个 0.7–2.6G | `docker image prune -f` |
| 旧版本镜像 | 每个 160M | 保留最近 2 个 + 回滚 tag |
| `frontend/node_modules` | 300–700M | 构建容器内产生，不落宿主盘 |
| 构建日志 `/tmp/build_*.log` | 每个 5–10M | 定期清 |

曾经因为残余镜像把盘写满（59G 用到 53G）导致全部服务挂掉。清理后回到 63%。

```bash
# 安全清理（只删悬空）
docker image prune -f
# 查看可回收
docker system df
```

## 8. 前端本地开发

```bash
cd frontend
pnpm install          # 服务器上 node_modules 被清理过，需要重装
pnpm run typecheck    # vue-tsc --noEmit
pnpm run check:i18n   # 校验 zh/en key 完整
pnpm run build        # build-studio → check:i18n → vue-tsc -b → vite build
```

`pnpm run build` 里 `build-studio.mjs` 在前、`vue-tsc -b` 在后，
本地改组件后跑一遍能提前发现类型错误，不用等 10 分钟镜像构建。

## 9. 后端本地编译

```bash
cd backend
go build ./...                 # 约 6 分钟（首次）
go build ./internal/... -v     # 只编自己改的包，快很多
```

编译要后台跑：

```bash
setsid nohup bash -c '
  cd /root/dev/ikun2api/backend &&
  go build ./... > /tmp/build.log 2>&1
  echo "EXIT=$?" >> /tmp/build.log
  touch /tmp/build.done
' < /dev/null > /dev/null 2>&1 &
```