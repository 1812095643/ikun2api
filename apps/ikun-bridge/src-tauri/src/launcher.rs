use crate::{
    platform,
    settings::{self, Settings},
    store::{Secrets, Store},
};
use anyhow::{ensure, Context, Result};
use serde::Serialize;
use std::{path::PathBuf, process::Command};
#[derive(Clone, Serialize)]
pub struct CheckItem {
    pub label: String,
    pub state: String,
    pub detail: String,
}
#[derive(Clone, Serialize)]
pub struct Inspection {
    pub items: Vec<CheckItem>,
    pub cli: String,
    pub desktops: Vec<platform::DesktopInstall>,
    pub config: String,
    pub ready: bool,
}
pub fn cli_executable(settings: &Settings) -> Result<PathBuf> {
    let path = if settings.cli_path.is_empty() {
        platform::find_cli().context("未找到 Claude Code 程序")?
    } else {
        PathBuf::from(&settings.cli_path)
    };
    let path = std::fs::canonicalize(path).context("无法访问 Claude Code 程序")?;
    ensure!(path.is_file(), "CLI 路径必须是程序文件");
    if cfg!(windows) {
        ensure!(
            path.file_name()
                .is_some_and(|n| n.to_string_lossy().eq_ignore_ascii_case("claude.exe")),
            "请选择 Claude Code 的原生 claude.exe"
        );
    }
    Ok(path)
}
pub fn inspect(settings: &Settings, store: &Store) -> Result<Inspection> {
    let cli = if settings.cli_path.is_empty() {
        platform::find_cli()
    } else {
        Some(PathBuf::from(&settings.cli_path))
    };
    let config = settings::cli_path(settings)?;
    let mut items = vec![];
    items.push(CheckItem {
        label: "Claude Code".into(),
        state: if cli.as_ref().is_some_and(|p| p.is_file()) {
            "ok"
        } else {
            "missing"
        }
        .into(),
        detail: cli
            .as_ref()
            .map(|p| p.display().to_string())
            .unwrap_or("未找到，设置中选择程序".into()),
    });
    let conflict = settings::check_cli_profile(settings);
    let valid = conflict.is_ok();
    let configured_fields = if valid && config.is_file() {
        store.existing_secrets()?.is_some_and(|secrets| {
            settings::prepare_cli(settings, &secrets)
                .is_ok_and(|f| f.iter().all(|r| r.changes.is_empty()))
        })
    } else {
        false
    };
    items.push(CheckItem {
        label: "CLI 配置".into(),
        state: if configured_fields {
            "ok"
        } else if valid {
            "pending"
        } else {
            "error"
        }
        .into(),
        detail: match conflict {
            Err(e) => e.to_string(),
            Ok(_) if configured_fields => "URL、更新和遥测配置已写入".into(),
            Ok(_) => "配置可修改，等待启动写入".into(),
        },
    });
    let configured = !settings.gateway_url.is_empty()
        && store
            .existing_secrets()?
            .is_some_and(|s| !s.gateway_key.is_empty());
    items.push(CheckItem {
        label: "网关".into(),
        state: if configured { "ok" } else { "missing" }.into(),
        detail: if configured {
            "已保存网关地址与 Key".into()
        } else {
            "尚未配置".into()
        },
    });
    let guarded = cli
        .as_ref()
        .is_some_and(|p| platform::guard_present(&store.root, p, settings.listen_port));
    items.push(CheckItem {
        label: "CLI 出口限制".into(),
        state: if guarded { "ok" } else { "pending" }.into(),
        detail: if guarded {
            "Windows 系统规则已安装".into()
        } else if cfg!(target_os = "macos") {
            "macOS 出口限制尚待实机验证".into()
        } else {
            "启动时安装系统规则".into()
        },
    });
    let desktops = platform::find_desktop().unwrap_or_default();
    items.push(CheckItem {
        label: "Claude Desktop".into(),
        state: if desktops.is_empty() && settings.desktop_path.is_empty() {
            "missing"
        } else {
            "pending"
        }
        .into(),
        detail: "可识别安装；安全启动尚未启用".into(),
    });
    Ok(Inspection {
        ready: cfg!(windows) && valid && configured && cli_executable(settings).is_ok(),
        cli: cli.map(|p| p.display().to_string()).unwrap_or_default(),
        config: config.display().to_string(),
        items,
        desktops,
    })
}
pub fn launch_cli(settings: &Settings, store: &Store, secrets: &Secrets) -> Result<u32> {
    ensure!(cfg!(windows), "当前平台尚未验证出口限制，已停止启动");
    let executable = cli_executable(settings)?;
    let config = settings::check_cli_profile(settings)?;
    let proxy = settings.proxy_url();
    let cwd = settings::cli_workdir(settings)?;
    let files = settings::prepare_cli(settings, secrets)?;
    platform::ensure_guard(&store.root, &executable, settings.listen_port)?;
    let journal = store.apply("cli", files)?;
    let mut cmd = Command::new(&executable);
    configure_env(&mut cmd, &proxy, store, secrets);
    cmd.env(
        "CLAUDE_CONFIG_DIR",
        config.parent().context("缺少 CLI 配置目录")?,
    );
    cmd.current_dir(&cwd);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        cmd.creation_flags(0x00000010);
    }
    {
        match cmd.spawn() {
            Ok(child) => Ok(child.id()),
            Err(e) => {
                if !journal.is_empty() {
                    let _ = store.undo(&journal);
                }
                Err(e.into())
            }
        }
    }
}
pub fn configure_env(cmd: &mut Command, proxy: &str, store: &Store, secrets: &Secrets) {
    for key in [
        "CLAUDE_CODE_USE_GATEWAY",
        "CLAUDE_CODE_USE_BEDROCK",
        "CLAUDE_CODE_USE_VERTEX",
        "CLAUDE_CODE_USE_FOUNDRY",
        "CLAUDE_CODE_USE_MANTLE",
        "CLAUDE_CODE_USE_ANTHROPIC_AWS",
        "CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD",
        "_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL",
        "NODE_TLS_REJECT_UNAUTHORIZED",
        "ANTHROPIC_CUSTOM_HEADERS",
        "ANTHROPIC_UNIX_SOCKET",
        "CLAUDE_CODE_OAUTH_TOKEN",
        "CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR",
    ] {
        cmd.env_remove(key);
    }
    cmd.env("ANTHROPIC_BASE_URL", "https://api.anthropic.com")
        .env("ANTHROPIC_API_KEY", &secrets.local_token)
        .env("ANTHROPIC_AUTH_TOKEN", &secrets.local_token);
    for (k, v) in [
        ("DISABLE_AUTOUPDATER", "1"),
        ("DISABLE_TELEMETRY", "1"),
        ("DISABLE_ERROR_REPORTING", "1"),
        ("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1"),
        ("CLAUDE_CODE_DISABLE_FEATURE_FLAGS", "1"),
        ("CLAUDE_CODE_ENABLE_TELEMETRY", "0"),
    ] {
        cmd.env(k, v);
    }
    for k in [
        "HTTP_PROXY",
        "HTTPS_PROXY",
        "http_proxy",
        "https_proxy",
        "ALL_PROXY",
        "all_proxy",
    ] {
        cmd.env(k, proxy);
    }
    for k in ["NO_PROXY", "no_proxy"] {
        cmd.env(k, "localhost,127.0.0.1,::1");
    }
    cmd.env("NODE_EXTRA_CA_CERTS", store.root.join("bridge-ca.pem"))
        .env("SSL_CERT_FILE", store.root.join("bridge-ca.pem"));
}
