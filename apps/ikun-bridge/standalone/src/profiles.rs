use crate::gateway::{normalize_gateway, validate_credential};
use anyhow::{ensure, Context, Result};
use ikun_bridge_core::store::Store;
use serde::{Deserialize, Serialize};
use std::path::PathBuf;
use uuid::Uuid;
use zeroize::Zeroize;

const FILE: &str = "connections.bin";

#[derive(Default, Deserialize, Serialize)]
struct Profiles {
    selected: Option<String>,
    entries: Vec<ConnectionProfile>,
}

#[derive(Clone, Deserialize, Serialize)]
pub struct ConnectionProfile {
    pub id: String,
    pub name: String,
    pub gateway: String,
    pub key: String,
}

impl Drop for ConnectionProfile {
    fn drop(&mut self) {
        self.key.zeroize();
    }
}

#[derive(Deserialize)]
pub struct ProfileInput {
    pub id: Option<String>,
    pub name: String,
    pub gateway: String,
    pub key: Option<String>,
}

impl Drop for ProfileInput {
    fn drop(&mut self) {
        self.key.zeroize();
    }
}

#[derive(Serialize)]
pub struct ProfileSummary {
    pub id: String,
    pub name: String,
    pub gateway: String,
    pub key_hint: String,
}

#[derive(Serialize)]
pub struct ProfileList {
    pub selected: Option<String>,
    pub entries: Vec<ProfileSummary>,
}

pub struct Repository {
    store: Store,
    profiles: Profiles,
}

impl Repository {
    pub fn open(root: PathBuf) -> Result<Self> {
        // 与本地证书复用系统密钥保护：Windows DPAPI，macOS 钥匙串密钥 + AES-GCM。
        let store = Store::new(root.join("official-api"))?;
        let path = store.root.join(FILE);
        let profiles: Profiles = match path.metadata() {
            Ok(meta) => {
                ensure!(meta.len() < 512 * 1024, "连接配置过大，请检查本机配置文件");
                store
                    .read_sealed(&path)
                    .context("无法读取已保存的连接，请检查系统密钥保护")?
            }
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => Profiles::default(),
            Err(_) => anyhow::bail!("无法读取连接配置"),
        };
        Ok(Self { store, profiles })
    }

    pub fn list(&self) -> ProfileList {
        ProfileList {
            selected: self.profiles.selected.clone(),
            entries: self
                .profiles
                .entries
                .iter()
                .map(|entry| ProfileSummary {
                    id: entry.id.clone(),
                    name: entry.name.clone(),
                    gateway: entry.gateway.clone(),
                    key_hint: if entry.key.len() <= 8 {
                        "••••".into()
                    } else {
                        format!("••••{}", entry.key.get(entry.key.len() - 4..).unwrap_or(""))
                    },
                })
                .collect(),
        }
    }

    pub fn get(&self, id: &str) -> Result<ConnectionProfile> {
        self.profiles
            .entries
            .iter()
            .find(|entry| entry.id == id)
            .cloned()
            .context("此连接已不存在，请重新选择")
    }

    pub fn save(&mut self, input: ProfileInput) -> Result<ProfileList> {
        let previous = input.id.as_deref().map(|id| self.get(id)).transpose()?;
        let gateway = normalize_gateway(&input.gateway).map_err(anyhow::Error::msg)?;
        let key = match input
            .key
            .as_deref()
            .map(str::trim)
            .filter(|key| !key.is_empty())
        {
            Some(key) => key.to_owned(),
            None => previous
                .as_ref()
                .map(|entry| entry.key.clone())
                .context("请填写 API Key")?,
        };
        validate_credential(&key).map_err(anyhow::Error::msg)?;
        let name = input.name.trim();
        ensure!(
            name.chars().count() <= 80 && !name.chars().any(char::is_control),
            "名称请控制在 80 个字以内"
        );
        let id = previous
            .as_ref()
            .map_or_else(|| Uuid::new_v4().to_string(), |entry| entry.id.clone());
        let entry = ConnectionProfile {
            id: id.clone(),
            name: if name.is_empty() {
                gateway.trim_start_matches("https://").to_owned()
            } else {
                name.to_owned()
            },
            gateway,
            key,
        };
        if let Some(index) = self
            .profiles
            .entries
            .iter()
            .position(|entry| entry.id == id)
        {
            self.profiles.entries[index] = entry;
        } else {
            ensure!(
                self.profiles.entries.len() < 100,
                "最多保存 100 套连接，请先移除不再使用的连接"
            );
            self.profiles.entries.push(entry);
        }
        self.profiles.selected = Some(id);
        self.persist()?;
        Ok(self.list())
    }

    pub fn select(&mut self, id: &str) -> Result<()> {
        self.get(id)?;
        if self.profiles.selected.as_deref() != Some(id) {
            self.profiles.selected = Some(id.to_owned());
            self.persist()?;
        }
        Ok(())
    }

    pub fn delete(&mut self, id: &str) -> Result<ProfileList> {
        self.get(id)?;
        self.profiles.entries.retain(|entry| entry.id != id);
        if self.profiles.selected.as_deref() == Some(id) {
            self.profiles.selected = self.profiles.entries.first().map(|entry| entry.id.clone());
        }
        self.persist()?;
        Ok(self.list())
    }

    fn persist(&self) -> Result<()> {
        self.store
            .write_sealed(&self.store.root.join(FILE), &self.profiles)
            .context("连接未保存，请检查系统密钥保护与目录权限")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn input(id: Option<String>, name: &str, gateway: &str, key: Option<&str>) -> ProfileInput {
        ProfileInput {
            id,
            name: name.into(),
            gateway: gateway.into(),
            key: key.map(str::to_owned),
        }
    }

    #[test]
    fn profiles_remember_keys_selection_edits_and_deletion_without_plaintext() {
        let root = tempfile::tempdir().unwrap();
        let mut repo = Repository::open(root.path().to_owned()).unwrap();
        let first = repo
            .save(input(
                None,
                "日常",
                "https://one.example/v1/",
                Some("fixture-first-private-key"),
            ))
            .unwrap()
            .selected
            .unwrap();
        let second = repo
            .save(input(
                None,
                "备用",
                "https://two.example",
                Some("fixture-second-private-key"),
            ))
            .unwrap()
            .selected
            .unwrap();
        repo.save(input(
            Some(first.clone()),
            "日常改名",
            "https://one.example",
            None,
        ))
        .unwrap();
        assert_eq!(repo.get(&first).unwrap().key, "fixture-first-private-key");
        repo.select(&second).unwrap();
        drop(repo);
        let mut repo = Repository::open(root.path().to_owned()).unwrap();
        let list = repo.list();
        assert_eq!(list.entries.len(), 2);
        assert_eq!(list.selected.as_deref(), Some(second.as_str()));
        let summary = serde_json::to_string(&list).unwrap();
        assert!(!summary.contains("fixture-first-private-key"));
        let encrypted = std::fs::read(root.path().join("official-api").join(FILE)).unwrap();
        assert!(!encrypted
            .windows(25)
            .any(|bytes| bytes == b"fixture-first-private-key"));
        repo.delete(&second).unwrap();
        assert_eq!(repo.list().selected.as_deref(), Some(first.as_str()));
        assert!(repo
            .save(input(None, "缺少密钥", "https://three.example", None))
            .is_err());
        assert!(repo
            .save(input(Some(first.clone()), "", "http://one.example", None))
            .is_err());
        assert_eq!(
            Repository::open(root.path().to_owned())
                .unwrap()
                .list()
                .entries
                .len(),
            1
        );
    }
}
