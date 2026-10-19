# 客户端协议采样与配置

## 分清三个入口

浏览器登录使用 Firefox 的网络栈；Claude Code / Codex 使用各自官方客户端的网络栈；IKUN2api 通过 Go 网关转发。网页 JavaScript 只能提供网页环境相关线索，不能替代官方 CLI 的 TLS 实采，也不能证明供应商的封禁原因。

模板应来源于目标服务器上独立运行的客户端。不要把他人的设备 ID、账号 UUID、会话 ID、Cookie 或 Token 当作通用模板。也不要根据入站用户使用 Windows，就把 Linux 网关所有身份字段硬改成 Windows。

## 采集方法

工具位于 `tools/client-profile/capture_native.py`，TLS 解析入口位于 `backend/cmd/clienthello-inspect/main.go`。它们复用现有官方客户端和 uTLS 解析，不重新实现协议。

先自行从官方渠道安装准备核对的客户端版本，核对来源与完整性，再显式传入二进制路径。工具不附带官方客户端安装包。

```sh
export IKUN_CAPTURE_CLAUDE_BINARY=/absolute/path/to/claude
export IKUN_CAPTURE_CODEX_BINARY=/absolute/path/to/codex
export IKUN_CAPTURE_OUTPUT_DIR=/root/ikun2api-capture
python3 tools/client-profile/capture_native.py claude codex
```

工具使用临时 HOME、独立配置目录、本地回环接收端和测试凭据。HTTP 采样只保存允许的请求头、头顺序和有限的协议字段；TLS 采样保存客户端向回环端口发出的第一条 ClientHello 记录。不会读取现有浏览器登录或 OAuth 缓存。

需要严格阻止客户端连接外网时，在专用 Linux 网络命名空间中执行，先启用其中的回环网卡：

```sh
sudo --preserve-env=IKUN_CAPTURE_CLAUDE_BINARY,IKUN_CAPTURE_CODEX_BINARY,IKUN_CAPTURE_OUTPUT_DIR unshare --net sh -c 'ip link set lo up; python3 tools/client-profile/capture_native.py claude codex'
```

旧 CPU 若不能执行已安装的 Claude 二进制，可在安装 `qemu-user` 后设置 `IKUN_CAPTURE_QEMU=true`；该方式使用 `qemu-x86_64 -cpu Nehalem` 和关闭 JIT 的兼容运行，必须在记录中注明，不能称作完全原生 CPU 采样。

解析 TLS 记录：

```sh
cd backend
go run ./cmd/clienthello-inspect /root/ikun2api-capture/claude-clienthello.bin /root/ikun2api-capture/codex-clienthello.bin
```

结果写在原文件旁的 `.json`，包含密码套件、扩展顺序、曲线、签名算法、ALPN、TLS 版本、key share 分组及 PSK 模式。

## 应用到管理端

在账号页的 TLS 指纹模板管理中新增独立模板，依据本次采样填写 `cipher_suites`、`curves`、`point_formats`、`signature_algorithms`、`alpn_protocols`、`supported_versions`、`key_share_groups`、`psk_modes` 与 `extensions`。数组顺序需保留；GREASE 开关以实际采样为准。

已有管理 API 为 `POST /api/v1/admin/tls-fingerprint-profiles` 与 `PUT /api/v1/admin/tls-fingerprint-profiles/:id`。账号扩展字段为 `enable_tls_fingerprint`、`tls_fingerprint_profile_id`。通过管理端保存可复用现有的缓存失效与更新通知，避免只改数据库导致进程继续使用旧模板。

Claude / Codex 的声明版本通过各自版本设置管理；已有账号的持久缓存可能优先于新默认值。修改后还需核对该账号真实出站使用的 User-Agent、SDK / runtime、OS / 架构、TLS 模板和入口是否对应。切勿批量删除缓存重造设备身份。

## 已有参考与未验收项

此前离线参考为 Claude Code `2.1.299` 与 Codex `0.163.0`。Claude 的参考入口是 API Key 模式的 `sdk-cli`，不能代替交互式 CLI、浏览器登录或真实 OAuth 会话的证据。版本号只是历史参考，不代表这些版本永远可用或应当固定使用。

真实 OAuth 采样当前暂停。没有完成账号身份、全会话生命周期和供应商响应的联合验收；不会据此宣称具有防封效果。
