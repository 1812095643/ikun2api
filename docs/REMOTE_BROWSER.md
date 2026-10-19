# 独立远程浏览器

配置方案为 Firefox + Openbox + TigerVNC + noVNC / websockify + Nginx HTTPS。浏览器在服务器上运行，访问者只接收桌面画面；浏览器账号会话持续保存在独立 profile 中。

本目录整理的是已有部署使用的方法。公开版本去掉了实际服务器地址、代理凭据、Cookie、VNC 密码和证书私钥，不直接导出生产配置。

## 网络边界

- 浏览器使用独立系统用户，固定连接本机 `127.0.0.1:18080` 的 HTTP 代理。
- 本机 Xray 将请求转发至配置文件中的 SOCKS5 上游。没有额外安装 Clash，也没有直连备用出口。
- Firefox 关闭 DoH、DNS 预取及代理故障直连；浏览器用户的系统防火墙仅放行本机代理及必要的已建立回环通信。
- VNC `5902` 和 websockify `6082` 仅监听回环。外部只开放带 HTTPS 与身份验证的 `9443`。
- WebRTC 设置 `media.peerconnection.ice.proxy_only=true`，同时由系统防火墙限制直连。配置生效后仍需检查实际 ICE 候选；不能只凭配置文件判断所有网页的行为。

代理断开时，浏览器应失去外网访问能力。不要为“恢复可用”加入直连回退。

## 新服务器配置

适用于 Ubuntu 24.04。先安装 Firefox 之外的依赖，以及按官方方法安装的 Xray，并确保 `/usr/local/bin/xray` 可用。

```sh
sudo apt-get update
sudo apt-get install -y ca-certificates curl gnupg python3 xauth x11-utils dbus-x11 openbox tigervnc-standalone-server tigervnc-tools websockify nginx iptables locales fonts-noto-cjk fonts-noto-color-emoji
sudo localedef -i zh_SG -f UTF-8 zh_SG.UTF-8
sudo localedef -i en_SG -f UTF-8 en_SG.UTF-8
sudo python3 tools/remote-browser/install_browser.py
```

安装脚本从 Mozilla 官方软件源安装 Firefox，并核对源签名密钥；noVNC 来自其官方 GitHub 发布归档。下载的程序、归档和运行资料只放在目标服务器，不进入本仓库。

准备属于自己的 TLS 证书和私有配置文件：

```sh
sudo install -m 600 tools/remote-browser/settings.example.json /root/ikun-browser-settings.json
sudoedit /root/ikun-browser-settings.json
sudo python3 tools/remote-browser/configure_browser.py /root/ikun-browser-settings.json
```

填写 `server_name`、证书路径及自己的 SOCKS5 地址和凭据。脚本会配置系统服务、Nginx 和浏览器用户的防火墙；不会修改已有 SSH 登录方式。不要直接在已有其他 Firefox 全局策略的机器上覆盖执行；脚本遇到冲突会停止。

首次生成的访问资料保存在服务器的 `/opt/ikun2api-remote-browser/access.json`，权限 `0600`。只在自己的终端查看并保存，不提交仓库，也不要把用户名和密码拼入浏览器 URL。首次启动的浏览器条款由使用者自行阅读和处理。

## 会话与显示

持久目录为 `/var/lib/ikun2api-browser/profile`，不轮换设备标识，不清理登录 Cookie。桌面为 `1366×900`，禁止远程连接自动改变屏幕尺寸；启用退出后的会话恢复。

时区为 `Asia/Singapore`，语言为 `zh-SG,en-SG,en`。地理定位 API 默认拒绝，不伪造 GPS。采用服务器实际 Firefox、系统字体和硬件环境，不注入 Canvas、WebGL 或平台伪造脚本。

浏览器设置、模型网关 TLS 模板和官方 CLI 是三个独立入口。修改浏览器语言不会自动改变网关的客户端身份。时区与语言也不能证明账号的国籍或供应商认可的地区。

## 可选服务器加固

`ipv6.conf.example` 是全接口禁用 IPv6 的示例；`sshd.local.example` 是 Fail2ban 的 SSH 防爆破配置。应用前确认服务器管理链路使用 IPv4，并在 Fail2ban 中加入实际管理员地址。保留一条 SSH 连接，再用另一条新连接确认可登录。

```sh
sudo apt-get install -y fail2ban
sudo install -m 600 tools/remote-browser/sshd.local.example /etc/fail2ban/jail.d/ikun2api-sshd.local
sudo fail2ban-client -t
sudo systemctl enable --now fail2ban
sudo systemctl restart fail2ban
```

IPv6 示例不会由浏览器安装脚本自动应用；应先检查现有服务的 IPv6 依赖。防火墙规则只限制专用浏览器用户，不用于阻断宿主 SSH、数据库或模型网关。

## 验收

检查浏览器实际的 `navigator.languages`、`Intl.DateTimeFormat().resolvedOptions().timeZone`、屏幕尺寸、出口 IP 和 WebRTC 候选。然后停止专用代理，确认浏览器访问外网失败；恢复代理后确认可继续使用。最后重启浏览器服务，确认 profile 与登录会话仍在。

公开脚本已做语法检查；已有服务器的配置记录与新机器从零安装是不同证据。没有完成的网页运行态、真实 OAuth 和重启后检查，不能写成已经验证。
