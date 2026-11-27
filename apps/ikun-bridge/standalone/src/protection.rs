use crate::local_profile;
use anyhow::{Context, Result};
use ikun_bridge_core::{
    config_edit::{Edit, FileRecord, Target},
    platform, proxy,
    settings::Settings,
    store::{Secrets, Store},
};
use std::path::{Path, PathBuf};

pub struct Protection {
    pub proxy: proxy::RunningProxy,
    pub store: Store,
    pub secrets: Secrets,
}

impl Drop for Protection {
    fn drop(&mut self) {
        self.proxy.cancel.cancel();
    }
}

impl Protection {
    pub async fn start(root: &Path, relay: &str, relay_key: &str) -> Result<Self> {
        let store = Store::new(root.join("official-api"))?;
        let mut secrets = store.secrets()?;
        // 客户端只持有本机凭据；真实网关 Key 始终留在 Gateway 会话中。
        secrets.local_token = format!("sk-ant-bridge-{relay_key}");
        secrets.gateway_key = relay_key.to_owned();
        proxy::ensure_certificates(&store, &mut secrets)?;
        let binding = store.root.join("binding");
        std::fs::create_dir_all(&binding)?;
        let (listener, _) = local_profile::bind(&binding).map_err(anyhow::Error::msg)?;
        let settings = Settings {
            gateway_url: relay.to_owned(),
            listen_port: listener.local_addr()?.port(),
            strict_egress: false,
            ..Default::default()
        };
        let proxy = proxy::start_on_listener(&settings, &secrets, true, listener).await?;
        Ok(Self {
            proxy,
            store,
            secrets,
        })
    }

    pub fn url(&self) -> String {
        format!("http://127.0.0.1:{}", self.proxy.port)
    }

    pub fn ca_file(&self) -> PathBuf {
        self.store.root.join("bridge-ca.pem")
    }

    pub fn prepare_desktop_trust(&self) -> Result<()> {
        // Electron 与其内置 CLI 使用系统 CA；只登记本工具自己的当前用户证书。
        platform::install_user_certificate(&self.store.root)
    }

    pub fn configure_cli(&self) -> Result<String> {
        let settings = Settings {
            listen_port: self.proxy.port,
            strict_egress: false,
            ..Default::default()
        };
        let path = ikun_bridge_core::settings::check_cli_profile(&settings)?;
        let mut edits = ikun_bridge_core::settings::cli_changes(&settings, &self.secrets);
        edits.retain(|edit| edit.target != Target::field(&["env", "ANTHROPIC_AUTH_TOKEN"]));
        edits.push(Edit {
            target: Target::field(&["env", "ANTHROPIC_AUTH_TOKEN"]),
            value: None,
        });
        edits.push(Edit::set(
            &["env", "NODE_EXTRA_CA_CERTS"],
            self.ca_file().display().to_string(),
        ));
        let record = FileRecord::prepare(&path, &edits, true).context("无法安全修改 CLI 配置")?;
        self.store.apply("cli", vec![record])
    }

    pub fn restore_cli(&self, id: &str) -> Result<()> {
        if !id.is_empty() {
            anyhow::ensure!(
                self.store.undo(id)?.is_empty(),
                "CLI 配置有手动修改，恢复记录已保留"
            );
        }
        Ok(())
    }
}
