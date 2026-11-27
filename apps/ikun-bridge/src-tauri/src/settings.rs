use crate::{
    config_edit::{atomic_write, Document, Edit, FileRecord, Target},
    store::Secrets,
};
use anyhow::{ensure, Context, Result};
use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};
pub const PROFILE_ID: &str = "e1b8d905-58b2-49e5-a9c7-97fc06ba5001";
#[derive(Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct Settings {
    pub gateway_url: String,
    pub upstream_proxy: String,
    pub listen_port: u16,
    pub cli_path: String,
    pub cli_config: String,
    pub cli_cwd: String,
    pub desktop_path: String,
    pub desktop_config_root: String,
    pub strict_egress: bool,
}
impl Default for Settings {
    fn default() -> Self {
        Self {
            gateway_url: String::new(),
            upstream_proxy: String::new(),
            listen_port: 19575,
            cli_path: String::new(),
            cli_config: String::new(),
            cli_cwd: String::new(),
            desktop_path: String::new(),
            desktop_config_root: String::new(),
            strict_egress: true,
        }
    }
}
impl Settings {
    pub fn load(root: &Path) -> Result<Self> {
        let path = root.join("settings.json");
        if path.exists() {
            Ok(serde_json::from_slice(&std::fs::read(path)?)?)
        } else {
            Ok(Self::default())
        }
    }
    pub fn save(&self, root: &Path) -> Result<()> {
        self.validate()?;
        atomic_write(
            &root.join("settings.json"),
            &serde_json::to_vec_pretty(self)?,
        )
    }
    pub fn proxy_url(&self) -> String {
        format!("http://127.0.0.1:{}", self.listen_port)
    }
    pub fn validate(&self) -> Result<()> {
        ensure!(self.listen_port >= 1024, "本地端口必须在 1024–65535 之间");
        if !self.gateway_url.is_empty() {
            gateway_url(&self.gateway_url, false)?;
        }
        if !self.upstream_proxy.is_empty() {
            let u = url::Url::parse(&self.upstream_proxy)?;
            ensure!(
                matches!(u.scheme(), "http" | "https"),
                "出口代理仅支持 HTTP / HTTPS"
            );
            ensure!(
                u.username().is_empty() && u.password().is_none(),
                "出口代理 URL 不应包含密码"
            );
        }
        Ok(())
    }
}
pub fn gateway_url(raw: &str, fixture: bool) -> Result<url::Url> {
    let u = url::Url::parse(raw).context("网关地址无效")?;
    ensure!(
        u.username().is_empty()
            && u.password().is_none()
            && u.query().is_none()
            && u.fragment().is_none(),
        "网关地址不能携带账号、查询参数或片段"
    );
    ensure!(
        u.scheme() == "https"
            || (fixture
                && u.scheme() == "http"
                && matches!(u.host_str(), Some("127.0.0.1" | "localhost"))),
        "网关必须使用 HTTPS"
    );
    ensure!(
        !matches!(u.host_str(), Some("api.anthropic.com" | "api.claude.ai")),
        "网关应填写 IKUN2api 地址"
    );
    Ok(u)
}
pub fn cli_path(settings: &Settings) -> Result<PathBuf> {
    if !settings.cli_config.is_empty() {
        let path = PathBuf::from(&settings.cli_config);
        ensure!(
            path.is_absolute() && path.file_name().is_some_and(|name| name == "settings.json"),
            "请选择绝对路径下的 Claude Code settings.json"
        );
        return Ok(path);
    }
    let config = std::env::var_os("CLAUDE_CONFIG_DIR")
        .map(PathBuf::from)
        .or_else(|| dirs::home_dir().map(|h| h.join(".claude")))
        .context("无法确定 Claude 配置目录")?;
    ensure!(config.is_absolute(), "Claude Code 配置目录必须是绝对路径");
    Ok(config.join("settings.json"))
}
pub fn cli_workdir(settings: &Settings) -> Result<PathBuf> {
    let path = if settings.cli_cwd.is_empty() {
        dirs::home_dir().context("无法确定启动目录")?
    } else {
        PathBuf::from(&settings.cli_cwd)
    };
    let path = std::fs::canonicalize(path).context("CLI 工作目录不存在")?;
    ensure!(path.is_dir(), "CLI 工作目录不存在");
    Ok(path)
}
pub fn check_cli_profile(settings: &Settings) -> Result<PathBuf> {
    let config = cli_path(settings)?;
    crate::platform::check_cli_policy()?;
    let cwd = cli_workdir(settings)?;
    let provider_keys = [
        "CLAUDE_CODE_USE_GATEWAY",
        "CLAUDE_CODE_USE_BEDROCK",
        "CLAUDE_CODE_USE_VERTEX",
        "CLAUDE_CODE_USE_FOUNDRY",
        "CLAUDE_CODE_USE_MANTLE",
        "CLAUDE_CODE_USE_ANTHROPIC_AWS",
        "CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD",
        "ANTHROPIC_UNIX_SOCKET",
    ];
    let route_keys = [
        "ANTHROPIC_BASE_URL",
        "ANTHROPIC_API_KEY",
        "ANTHROPIC_AUTH_TOKEN",
        "HTTPS_PROXY",
        "HTTP_PROXY",
        "ALL_PROXY",
        "NO_PROXY",
        "NODE_EXTRA_CA_CERTS",
        "SSL_CERT_FILE",
        "NODE_TLS_REJECT_UNAUTHORIZED",
        "ANTHROPIC_CUSTOM_HEADERS",
        "CLAUDE_CONFIG_DIR",
        "DISABLE_AUTOUPDATER",
        "DISABLE_TELEMETRY",
        "DISABLE_ERROR_REPORTING",
        "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC",
        "CLAUDE_CODE_DISABLE_FEATURE_FLAGS",
        "CLAUDE_CODE_ENABLE_TELEMETRY",
    ];
    let active = |v: &serde_json::Value| {
        !matches!(v, serde_json::Value::Null | serde_json::Value::Bool(false))
            && !v.as_str().is_some_and(|s| {
                matches!(
                    s.trim().to_ascii_lowercase().as_str(),
                    "" | "0" | "false" | "off"
                )
            })
    };
    let mut files = vec![(config.clone(), false)];
    for dir in cwd.ancestors() {
        files.extend(
            ["settings.local.json", "settings.json"]
                .map(|name| (dir.join(".claude").join(name), true)),
        );
    }
    for (file, project) in files {
        if !file.try_exists()? {
            continue;
        }
        if project && std::fs::canonicalize(&file).ok() == std::fs::canonicalize(&config).ok() {
            continue;
        }
        let data = Document::decode(&std::fs::read(&file)?, true)
            .with_context(|| format!("配置无法安全解析：{}", file.display()))?
            .value()?;
        ensure!(
            !data.get("apiKeyHelper").is_some_and(active),
            "配置包含 API Key 助手，请用独立配置启动：{}",
            file.display()
        );
        if let Some(env) = data.get("env") {
            let env = env.as_object().context("settings.json 的 env 必须是对象")?;
            for (key, value) in env {
                let key = key.to_ascii_uppercase();
                ensure!(
                    !(provider_keys.contains(&key.as_str()) && active(value)),
                    "配置指定了其他供应商或连接方式：{}（{key}）",
                    file.display()
                );
                ensure!(
                    !project || !route_keys.contains(&key.as_str()),
                    "项目会覆盖接管配置：{}（{key}）",
                    file.display()
                );
            }
        }
    }
    Ok(config)
}
pub fn cli_changes(settings: &Settings, secrets: &Secrets) -> Vec<Edit> {
    let mut pairs = vec![
        ("ANTHROPIC_BASE_URL", "https://api.anthropic.com".to_owned()),
        ("ANTHROPIC_API_KEY", secrets.local_token.clone()),
        ("ANTHROPIC_AUTH_TOKEN", secrets.local_token.clone()),
        ("DISABLE_AUTOUPDATER", "1".into()),
        ("DISABLE_TELEMETRY", "1".into()),
        ("DISABLE_ERROR_REPORTING", "1".into()),
        ("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1".into()),
        ("CLAUDE_CODE_DISABLE_FEATURE_FLAGS", "1".into()),
        ("CLAUDE_CODE_ENABLE_TELEMETRY", "0".into()),
        ("HTTPS_PROXY", settings.proxy_url()),
        ("HTTP_PROXY", settings.proxy_url()),
        ("https_proxy", settings.proxy_url()),
        ("http_proxy", settings.proxy_url()),
        ("NO_PROXY", "localhost,127.0.0.1,::1".into()),
        ("no_proxy", "localhost,127.0.0.1,::1".into()),
    ];
    pairs
        .drain(..)
        .map(|(k, v)| Edit::set(&["env", k], v))
        .collect()
}
pub fn prepare_cli(settings: &Settings, secrets: &Secrets) -> Result<Vec<FileRecord>> {
    let path = cli_path(settings)?;
    Ok(vec![FileRecord::prepare(
        &path,
        &cli_changes(settings, secrets),
        true,
    )?])
}
pub fn desktop_root(settings: &Settings) -> Result<PathBuf> {
    ensure!(
        !settings.desktop_config_root.is_empty(),
        "请先选择 Desktop 的有效本地配置目录"
    );
    let p = PathBuf::from(&settings.desktop_config_root);
    ensure!(p.is_absolute(), "Desktop 配置目录必须是绝对路径");
    Ok(p)
}
pub fn prepare_desktop(settings: &Settings, secrets: &Secrets) -> Result<Vec<FileRecord>> {
    let root = desktop_root(settings)?;
    let profile = root
        .join("configLibrary")
        .join(format!("{PROFILE_ID}.json"));
    let profile_edits = vec![
        Edit::set(&["inferenceProvider"], "anthropic"),
        Edit::set(&["inferenceCredentialKind"], "static"),
        Edit::set(&["inferenceAnthropicApiKey"], secrets.local_token.clone()),
        Edit::set(&["disableAutoUpdates"], true),
        Edit::set(&["disableEssentialTelemetry"], true),
        Edit::set(&["disableNonessentialTelemetry"], true),
        Edit::set(&["disableNonessentialServices"], true),
        Edit::set(&["egressProxyUrl"], settings.proxy_url()),
        Edit {
            target: Target::field(&["egressProxyPacUrl"]),
            value: None,
        },
    ];
    let entry = Edit {
        target: Target::Entry {
            path: vec!["entries".into()],
            id: PROFILE_ID.into(),
        },
        value: Some(serde_json::json!({"id":PROFILE_ID,"name":"IKUN Bridge"})),
    };
    Ok(vec![
        FileRecord::prepare(&profile, &profile_edits, false)?,
        FileRecord::prepare(
            &root.join("configLibrary/_meta.json"),
            &[entry, Edit::set(&["appliedId"], PROFILE_ID)],
            false,
        )?,
        FileRecord::prepare(
            &root.join("claude_desktop_config.json"),
            &[Edit::set(&["deploymentMode"], "3p")],
            false,
        )?,
    ])
}
