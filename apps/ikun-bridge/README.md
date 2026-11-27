# IKUN Bridge

Rust + Tauri 桌面客户端，支持 Windows x64 和 macOS Apple Silicon／Intel。当前版本 **0.2.6**，入口位于 `standalone`，共享的代理与配置处理核心位于 `src-tauri`。

## 使用

1. 在连接弹窗新增并保存网关地址和 API Key，可按名称管理多套连接。
2. 在 Claude Code 或 Claude Desktop 对应行点击「启动」，Bridge 自动配置并打开客户端。
3. 在底部处理状态查看准入检查、客户端配置、过滤统计及实际请求接管情况。

客户端使用官方 `https://api.anthropic.com` API Key 入口，经本机 TLS 代理过滤后转发至网关。本机代理地址与上游网关分别显示，真实网关 Key 由系统密钥保护加密保存，不写入浏览器存储，界面只显示掩码。重启后记住上次选择，直接启动客户端即可连接；切换时保留本地代理地址，进行中的请求继续使用原连接。首次启用 Desktop 时安装当前用户的 Bridge 证书信任，恢复配置时撤销。后续启动先核验系统对当前证书的实际信任，已信任则直接复用；首次授权、证书更换或信任被撤销时才需要系统确认。

CLI、Desktop 的齿轮弹窗提供「恢复原配置」。配置采用字段补丁，保留撤销记录，不覆盖用户整份 JSON；遇到手动修改冲突或系统托管策略时提示处理。旧版 Bridge 自建的 Gateway 配置在确认归属后自动迁移。

点击系统关闭按钮会隐藏到托盘；点击托盘或再次运行程序恢复窗口。通过界面内的「退出」结束服务。

分组开启「仅允许 IKUN Bridge」时，请搭配 **IKUN2api 1.6.5 或以上**使用本次验票修复。通行证由现有 Key 自动领取，不需另外创建凭据；领证、目录准入校验和实际客户端请求分别显示。

## 本地构建

安装 Node.js 24、Rust 1.95、Python 3.12 和当前平台的原生构建工具。Windows 需要 Visual Studio C++ 构建工具与 WebView2，macOS 需要 Xcode Command Line Tools。正式发布使用的依赖由两个 `Cargo.lock` 和 `package-lock.json` 锁定。

在本目录执行：

```sh
npm ci
npm run build:desktop
cargo test --manifest-path standalone/Cargo.toml --locked
cargo build --release --manifest-path standalone/Cargo.toml --locked
```

Windows 便携程序位于 `standalone/target/release/ikun-bridge.exe`。生成带版本号的交付文件和校验信息：

```sh
python standalone/tools/package.py --platform windows --binary standalone/target/release/ikun-bridge.exe --output standalone/target/distribution
```

在 macOS 原生构建通用应用：

```sh
rustup target add aarch64-apple-darwin x86_64-apple-darwin
MACOSX_DEPLOYMENT_TARGET=11.0 cargo build --release --locked --manifest-path standalone/Cargo.toml --target aarch64-apple-darwin
MACOSX_DEPLOYMENT_TARGET=11.0 cargo build --release --locked --manifest-path standalone/Cargo.toml --target x86_64-apple-darwin
lipo -create standalone/target/aarch64-apple-darwin/release/ikun-bridge standalone/target/x86_64-apple-darwin/release/ikun-bridge -output standalone/target/IKUNBridge
python3 standalone/tools/package.py --platform macos --binary standalone/target/IKUNBridge --output standalone/target/distribution
```

打包脚本创建 `.app`、设置执行权限、完成本地签名并验证，最终输出 Universal ZIP，附版本、提交号和 SHA-256 校验信息。

## GitHub 云编译

推送 `bridge-v0.2.6` 形式的标签触发根目录 `.github/workflows/bridge-release.yml`。Windows、macOS 在各自原生 runner 编译，通过后才公开对应的 Bridge Release。构建产物直接上传 Release，不占用 Actions artifact 存储配额。

Bridge Release 不设为仓库的 Latest，避免影响 IKUN2api 服务端自动更新。源码随主仓库提交，并包含在服务端源码发布包中。

## 验证范围

自动回归覆盖配置精确修改与撤销、本地 CONNECT／TLS、隐私过滤、通行证注入、模型目录准入、关闭隐藏与退出生命周期。`go_gateway_authenticates_the_actual_bridge_pass` 为显式启用的 Rust → Go 验票联调；`original_cli_uses_official_hostname_and_local_bridge` 需要 `IKUN_TEST_CLAUDE_EXE` 指向官方 CLI。两项默认跳过，测试使用本地接收器，不调用付费模型。

Windows 尚未做商业代码签名，macOS 使用本地签名、未做 Apple 公证。已有 Windows 原生交互和官方 CLI 隔离验证；完整 Desktop 会话、macOS 实机以及系统级全部出口防回落仍需分别验收。
