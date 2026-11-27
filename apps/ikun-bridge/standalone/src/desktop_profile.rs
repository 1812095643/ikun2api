use crate::{
    config_edit::{compare_write, strict_value, Change, Document, Edit, Target},
    local_profile,
};
use anyhow::{anyhow, ensure, Context, Result};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::{
    fs,
    io::{self, Read},
    path::{Path, PathBuf},
};
use uuid::Uuid;
use zeroize::Zeroizing;

const CONFIG: &str = "claude_desktop_config.json";
const UNDO: &str = "desktop-undo.bin";
const MAX_FILE: u64 = 2 * 1024 * 1024;

#[derive(Clone, Copy, PartialEq, Serialize, Deserialize)]
enum File {
    Standard,
    ThirdParty,
    Library,
}

pub struct DesktopProfile {
    data: PathBuf,
    bridge: PathBuf,
}

#[derive(Serialize, Deserialize)]
struct Undo {
    id: Uuid,
    profile_hash: [u8; 32],
    files: Vec<FileUndo>,
}

#[derive(Serialize, Deserialize)]
struct FileUndo {
    file: File,
    before: Option<[u8; 32]>,
    after: [u8; 32],
    edits: Vec<InverseEdit>,
}

// Keep only the bytes changed by Bridge. Other profile entries and user secrets
// are not copied into the durable undo record.
#[derive(Serialize, Deserialize)]
struct InverseEdit {
    change: Change,
    offset: usize,
    inserted: usize,
    removed: Vec<u8>,
}

struct Patch {
    path: PathBuf,
    before: Option<Vec<u8>>,
    after: Option<Vec<u8>>,
}

impl DesktopProfile {
    pub fn new(data: PathBuf, bridge: PathBuf) -> Self {
        Self { data, bridge }
    }

    fn path(&self, file: File) -> PathBuf {
        match file {
            File::Standard => self.data.join("Claude").join(CONFIG),
            File::ThirdParty => self.data.join("Claude-3p").join(CONFIG),
            File::Library => self.data.join("Claude-3p/configLibrary/_meta.json"),
        }
    }

    fn profile(&self, id: Uuid) -> PathBuf {
        self.path(File::Library)
            .with_file_name(format!("{id}.json"))
    }

    fn record(&self) -> PathBuf {
        self.bridge.join(UNDO)
    }

    pub fn has_undo(&self) -> bool {
        self.record().exists()
    }

    fn read_undo(&self) -> Result<Option<(Undo, Vec<u8>)>> {
        let Some(sealed) = read(&self.record())? else {
            return Ok(None);
        };
        let plain = local_profile::protect(&sealed, false)
            .map_err(|_| anyhow!("无法读取 Desktop 恢复记录"))?;
        let journal = serde_json::from_slice(&plain).context("Desktop 恢复记录无效")?;
        Ok(Some((journal, sealed)))
    }

    pub fn launch(&self, endpoint: &str, key: &str) -> Result<()> {
        crate::launch::check_desktop_policy()?;
        self.launch_with(
            endpoint,
            key,
            crate::launch::close_desktop,
            crate::launch::desktop,
        )
    }

    fn launch_with(
        &self,
        endpoint: &str,
        key: &str,
        close: impl FnOnce() -> Result<bool>,
        start: impl Fn() -> Result<(), String>,
    ) -> Result<()> {
        if let Some((undo, _)) = self.read_undo()? {
            let current = read(&self.profile(undo.id))?;
            let profile = fields(&self.profile(undo.id))?;
            let meta = fields(&self.path(File::Library))?;
            let selected = meta["appliedId"].as_str() == Some(undo.id.to_string().as_str());
            let legacy_key = key.strip_prefix("sk-ant-bridge-");
            let legacy_sealed = profile["inferenceGatewayApiKey"]
                .as_str()
                .is_some_and(|value| value.starts_with("__sealed__:v1:"));
            let sealed_legacy_matches = if legacy_sealed {
                let mut original = profile.clone();
                original["inferenceGatewayApiKey"] =
                    Value::String(legacy_key.unwrap_or_default().to_owned());
                hash(&serde_json::to_vec_pretty(&original)?) == undo.profile_hash
            } else {
                false
            };
            if selected
                && profile["inferenceProvider"] == "anthropic"
                && profile["egressProxyUrl"] == endpoint
                && profile["inferenceAnthropicApiKey"]
                    .as_str()
                    .is_some_and(|value| value.starts_with("__sealed__:v1:"))
            {
                // Desktop 会自动封装密钥。只重写本工具独立配置的本机凭据，不读取或解密原账号密钥。
                let bytes = current.as_deref().context("Desktop 配置已不存在")?;
                let mut doc = document(Some(bytes))?;
                doc.apply(&Edit::set(&["inferenceAnthropicApiKey"], key))?;
                let patch = Patch {
                    path: self.profile(undo.id),
                    before: current,
                    after: Some(doc.encode()),
                };
                let was_running = close()?;
                let result = commit(&[patch], || start().map_err(|error| anyhow!(error)));
                if result.is_err() && was_running {
                    let _ = start();
                }
                return result;
            }
            // 只自动迁移旧版由 Bridge 创建且未被改动的配置；不覆盖其他软件或用户的连接配置。
            if (current
                .as_ref()
                .is_some_and(|bytes| hash(bytes) == undo.profile_hash)
                || sealed_legacy_matches)
                && meta["appliedId"].as_str() == Some(undo.id.to_string().as_str())
                && profile["inferenceProvider"] == "gateway"
                && (profile["inferenceGatewayApiKey"].as_str() == legacy_key
                    || sealed_legacy_matches)
            {
                close()?;
                if sealed_legacy_matches {
                    // 旧配置只差 Desktop 自动封装的本机密钥，先恢复本工具持有的值再撤销旧事务。
                    let mut original = profile.clone();
                    original["inferenceGatewayApiKey"] =
                        Value::String(legacy_key.unwrap_or_default().to_owned());
                    compare_write(
                        &self.profile(undo.id),
                        current.as_deref(),
                        Some(&serde_json::to_vec_pretty(&original)?),
                    )?;
                }
                ensure!(
                    !self.restore_with(|| Ok(false), || Ok(()))?,
                    "旧版配置有手动修改，请先检查恢复结果"
                );
                return self.apply_new(endpoint, key, || Ok(false), start);
            }
            ensure!(
                self.is_applied(&undo, endpoint, key)?,
                "Desktop 配置已变更，请先在设置中恢复原配置"
            );
            return start().map_err(|error| anyhow!(error));
        }
        self.apply_new(endpoint, key, close, start)
    }

    fn apply_new(
        &self,
        endpoint: &str,
        key: &str,
        close: impl FnOnce() -> Result<bool>,
        start: impl Fn() -> Result<(), String>,
    ) -> Result<()> {
        let (undo, patches) = self.prepare(endpoint, key)?;
        let bytes = Zeroizing::new(serde_json::to_vec(&undo)?);
        let sealed = local_profile::protect(&bytes, true)
            .map_err(|_| anyhow!("无法保护 Desktop 恢复记录"))?;
        let was_running = close()?;
        let mut recorded = false;
        let result = (|| {
            local_profile::write_new(&self.record(), &sealed)
                .context("无法保存 Desktop 恢复记录")?;
            recorded = true;
            commit(&patches, || start().map_err(|error| anyhow!(error)))
        })();
        if let Err(error) = result {
            let rolled_back = !recorded
                || (patches.iter().all(|p| p.is_original())
                    && compare_write(&self.record(), Some(&sealed), None).is_ok());
            if rolled_back {
                if was_running {
                    let _ = start();
                }
                return Err(error);
            }
            return Err(error.context("Desktop 配置未完成，恢复记录已保留"));
        }
        Ok(())
    }

    fn is_applied(&self, undo: &Undo, endpoint: &str, key: &str) -> Result<bool> {
        let meta = fields(&self.path(File::Library))?;
        let id = undo.id.to_string();
        if meta["appliedId"].as_str() != Some(id.as_str())
            || !meta["entries"].as_array().is_some_and(|entries| {
                entries
                    .iter()
                    .any(|entry| entry["id"].as_str() == Some(id.as_str()))
            })
            || [File::Standard, File::ThirdParty].into_iter().any(|file| {
                fields(&self.path(file)).map_or(true, |doc| doc["deploymentMode"] != "3p")
            })
        {
            return Ok(false);
        }
        let profile = fields(&self.profile(undo.id))?;
        Ok(profile["inferenceProvider"] == "anthropic"
            && profile["inferenceCredentialKind"] == "static"
            && profile["egressProxyUrl"] == endpoint
            && profile["inferenceAnthropicApiKey"] == key)
    }

    fn prepare(&self, endpoint: &str, key: &str) -> Result<(Undo, Vec<Patch>)> {
        let id = Uuid::new_v4();
        let profile = Zeroizing::new(serde_json::to_vec_pretty(&json!({
            "inferenceProvider": "anthropic",
            "inferenceCredentialKind": "static",
            "inferenceAnthropicApiKey": key,
            "egressProxyUrl": endpoint,
            "disableAutoUpdates": true,
            "disableEssentialTelemetry": true,
            "disableNonessentialTelemetry": true,
            "disableNonessentialServices": true
        }))?);
        let mut undo = Undo {
            id,
            profile_hash: hash(&profile),
            files: vec![],
        };
        let mut patches = vec![Patch {
            path: self.profile(id),
            before: None,
            after: Some(profile.to_vec()),
        }];
        for file in [File::Library, File::ThirdParty, File::Standard] {
            let path = self.path(file);
            let before = read(&path)?;
            let mut doc = document(before.as_deref())?;
            let edits = if file == File::Library {
                let values = doc.value()?;
                ensure!(
                    values.get("entries").is_none_or(Value::is_array),
                    "Desktop 配置列表格式不正确"
                );
                ensure!(
                    values["appliedId"].is_null()
                        || values["appliedId"].as_str().is_some_and(|s| s.len() <= 128),
                    "Desktop 当前配置标识无效"
                );
                vec![
                    Edit {
                        target: Target::Entry {
                            path: vec!["entries".into()],
                            id: id.to_string(),
                        },
                        value: Some(json!({"id": id, "name": "IKUN Bridge"})),
                    },
                    Edit::set(&["appliedId"], id.to_string()),
                ]
            } else {
                let previous = doc.current(&Target::field(&["deploymentMode"]))?;
                ensure!(
                    previous
                        .as_ref()
                        .is_none_or(|v| v.is_null() || matches!(v.as_str(), Some("1p" | "3p"))),
                    "Desktop 部署模式由其他配置管理"
                );
                vec![Edit::set(&["deploymentMode"], "3p")]
            };
            let mut inverses = vec![];
            for edit in edits {
                let old = doc.encode();
                let change = doc.apply(&edit)?;
                if change.before != change.after {
                    inverses.push(InverseEdit::new(change, &old, &doc.encode()));
                }
            }
            if inverses.is_empty() && before.is_some() {
                continue;
            }
            let after = doc.encode();
            undo.files.push(FileUndo {
                file,
                before: before.as_ref().map(|b| hash(b)),
                after: hash(&after),
                edits: inverses,
            });
            patches.push(Patch {
                path,
                before,
                after: Some(after),
            });
        }
        // Check the new profile path as well as all shared paths before closing Desktop.
        ensure!(read(&patches[0].path)?.is_none(), "Desktop 配置标识冲突");
        Ok((undo, patches))
    }

    pub fn restore(&self) -> Result<bool> {
        self.restore_with(crate::launch::close_desktop, crate::launch::desktop)
    }

    fn restore_with(
        &self,
        close: impl FnOnce() -> Result<bool>,
        start: impl Fn() -> Result<(), String>,
    ) -> Result<bool> {
        let Some((undo, sealed)) = self.read_undo()? else {
            return Ok(false);
        };
        let profile_path = self.profile(undo.id);
        let profile = read(&profile_path)?;
        let mut keep_profile = profile
            .as_ref()
            .is_some_and(|bytes| hash(bytes) != undo.profile_hash);
        let meta = fields(&self.path(File::Library))?;
        let selection = &meta["appliedId"];
        let owned = selection.as_str() == Some(undo.id.to_string().as_str());
        let previous = undo
            .files
            .iter()
            .find(|file| file.file == File::Library)
            .and_then(|file| {
                file.edits
                    .iter()
                    .find(|e| matches!(e.change.target, Target::Field { .. }))
            })
            .and_then(|edit| edit.change.before.as_ref())
            .unwrap_or(&Value::Null);
        let keep_selection = !owned && selection != previous;
        let mut patches = vec![];
        let mut kept_edits = keep_selection || keep_profile;
        for file in undo.files.iter().rev() {
            if file.file != File::Library && keep_selection {
                continue;
            }
            let path = self.path(file.file);
            let Some(raw) = read(&path)? else { continue };
            let current_hash = hash(&raw);
            if file.before == Some(current_hash) {
                continue;
            }
            let after =
                if current_hash == file.after && !(keep_profile && file.file == File::Library) {
                    let mut original = raw.clone();
                    for edit in file.edits.iter().rev() {
                        edit.reverse(&mut original)?;
                    }
                    if let Some(original_hash) = file.before {
                        ensure!(hash(&original) == original_hash, "Desktop 恢复记录校验失败");
                        Some(original)
                    } else {
                        None
                    }
                } else {
                    kept_edits = true;
                    let mut doc = document(Some(&raw))?;
                    for edit in file.edits.iter().rev() {
                        let entry = matches!(edit.change.target, Target::Entry { .. });
                        if file.file == File::Library
                            && (entry && keep_profile || !entry && keep_selection)
                        {
                            continue;
                        }
                        if !doc.undo(&edit.change)? && entry {
                            keep_profile = true;
                        }
                    }
                    Some(doc.encode())
                };
            if after.as_deref() != Some(&raw) {
                patches.push(Patch {
                    path,
                    before: Some(raw),
                    after,
                });
            }
        }
        if let Some(bytes) = profile.filter(|_| !keep_profile) {
            patches.push(Patch {
                path: profile_path,
                before: Some(bytes),
                after: None,
            });
        }
        patches.push(Patch {
            path: self.record(),
            before: Some(sealed),
            after: None,
        });
        let running = close()?;
        let result = commit(&patches, || Ok(()));
        if running && (result.is_ok() || patches.iter().all(Patch::is_original)) {
            let started = start().map_err(|error| anyhow!(error));
            if result.is_ok() {
                started.context("原配置已恢复，但未能重新打开 Desktop")?;
            }
        }
        result.map(|_| kept_edits || keep_profile)
    }
}

impl InverseEdit {
    fn new(change: Change, before: &[u8], after: &[u8]) -> Self {
        let offset = before.iter().zip(after).take_while(|(a, b)| a == b).count();
        let tail = before[offset..]
            .iter()
            .rev()
            .zip(after[offset..].iter().rev())
            .take_while(|(a, b)| a == b)
            .count();
        Self {
            change,
            offset,
            inserted: after.len() - offset - tail,
            removed: before[offset..before.len() - tail].to_vec(),
        }
    }

    fn reverse(&self, bytes: &mut Vec<u8>) -> Result<()> {
        let end = self
            .offset
            .checked_add(self.inserted)
            .context("Desktop 恢复记录无效")?;
        ensure!(end <= bytes.len(), "Desktop 恢复记录无效");
        bytes.splice(self.offset..end, self.removed.iter().copied());
        Ok(())
    }
}

impl Patch {
    fn apply(&self) -> Result<()> {
        read(&self.path)?;
        compare_write(&self.path, self.before.as_deref(), self.after.as_deref())
    }
    fn is_original(&self) -> bool {
        read(&self.path).is_ok_and(|bytes| bytes == self.before)
    }
}

fn commit(patches: &[Patch], after: impl FnOnce() -> Result<()>) -> Result<()> {
    let mut written = vec![];
    let result = (|| {
        for patch in patches {
            patch.apply()?;
            written.push(patch);
        }
        after()
    })();
    if result.is_err() {
        for patch in written.into_iter().rev() {
            let _ = read(&patch.path).and_then(|_| {
                compare_write(&patch.path, patch.after.as_deref(), patch.before.as_deref())
            });
        }
    }
    result
}

fn read(path: &Path) -> Result<Option<Vec<u8>>> {
    // Do not follow symlinks or junctions into another app's files.
    for parent in path.ancestors().take_while(|p| !p.as_os_str().is_empty()) {
        match fs::symlink_metadata(parent) {
            Ok(meta) => {
                #[cfg(windows)]
                {
                    use std::os::windows::fs::MetadataExt;
                    ensure!(
                        meta.file_attributes() & 0x400 == 0,
                        "Desktop 配置路径包含重解析点"
                    );
                }
                ensure!(
                    !meta.file_type().is_symlink(),
                    "Desktop 配置路径包含符号链接"
                );
                ensure!(
                    if parent == path {
                        meta.is_file()
                    } else {
                        meta.is_dir()
                    },
                    "Desktop 配置路径类型无效"
                );
            }
            Err(error) if error.kind() == io::ErrorKind::NotFound => continue,
            Err(error) => return Err(error.into()),
        }
    }
    let file = match fs::File::open(path) {
        Ok(file) => file,
        Err(error) if error.kind() == io::ErrorKind::NotFound => return Ok(None),
        Err(error) => return Err(error.into()),
    };
    let mut bytes = vec![];
    file.take(MAX_FILE + 1).read_to_end(&mut bytes)?;
    ensure!(bytes.len() as u64 <= MAX_FILE, "Desktop 配置文件过大");
    Ok(Some(bytes))
}

fn document(bytes: Option<&[u8]>) -> Result<Document> {
    let doc = match bytes {
        Some(bytes) => Document::decode(bytes, false)?,
        None => Document::empty(false),
    };
    ensure!(doc.value()?.is_object(), "Desktop 配置必须是 JSON 对象");
    Ok(doc)
}

fn fields(path: &Path) -> Result<Value> {
    match read(path)? {
        Some(bytes) => strict_value(&bytes),
        None => Ok(json!({})),
    }
}

fn hash(bytes: &[u8]) -> [u8; 32] {
    Sha256::digest(bytes).into()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::Cell;

    const ENDPOINT: &str = "http://127.0.0.1:52171";
    const KEY: &str = "fixture-local-only";
    const STANDARD: &[u8] = b"{\r\n  \"deploymentMode\" : \"1p\", \"mcpServers\": {\"test\": {\"x\": \"keep me\"}}\r\n}\r\n";
    const THIRDPARTY: &[u8] = br#"{"preferences":{"keep":true},"deploymentMode":"1p"}"#;
    const META: &[u8] =
        br#"{"entries":[ {"id":"other",  "name":"Existing provider"} ], "appliedId" : "other" }"#;

    fn fixture() -> (tempfile::TempDir, DesktopProfile) {
        let dir = tempfile::tempdir().unwrap();
        let root = dir.path().canonicalize().unwrap();
        let desktop = DesktopProfile::new(root.join("apps"), root.join("bridge"));
        fs::create_dir(&desktop.bridge).unwrap();
        for (file, bytes) in [
            (File::Standard, STANDARD),
            (File::ThirdParty, THIRDPARTY),
            (File::Library, META),
        ] {
            let path = desktop.path(file);
            fs::create_dir_all(path.parent().unwrap()).unwrap();
            fs::write(path, bytes).unwrap();
        }
        (dir, desktop)
    }

    fn original(desktop: &DesktopProfile) {
        for (file, bytes) in [
            (File::Standard, STANDARD),
            (File::ThirdParty, THIRDPARTY),
            (File::Library, META),
        ] {
            assert_eq!(fs::read(desktop.path(file)).unwrap(), bytes);
        }
        assert!(!desktop.has_undo());
    }

    #[test]
    fn apply_reuse_and_restore_from_original_bytes() {
        let (_dir, desktop) = fixture();
        let old_provider = desktop.path(File::Library).with_file_name("other.json");
        let old_bytes = br#"{"inferenceGatewayApiKey":"fixture-previous-private"}"#;
        fs::write(&old_provider, old_bytes).unwrap();
        let launches = Cell::new(0);
        let open = || {
            launches.set(launches.get() + 1);
            let (undo, _) = desktop.read_undo().unwrap().unwrap();
            assert!(desktop.is_applied(&undo, ENDPOINT, KEY).unwrap());
            Ok(())
        };
        desktop
            .launch_with(ENDPOINT, KEY, || Ok(true), open)
            .unwrap();
        desktop
            .launch_with(
                ENDPOINT,
                KEY,
                || panic!("same configuration should not restart"),
                open,
            )
            .unwrap();
        assert_eq!(launches.get(), 2);
        let meta = fs::read_to_string(desktop.path(File::Library)).unwrap();
        assert!(meta.contains(r#"{"id":"other",  "name":"Existing provider"}"#));
        assert_eq!(fs::read(&old_provider).unwrap(), old_bytes);
        let (undo, _) = desktop.read_undo().unwrap().unwrap();
        assert!(!desktop
            .restore_with(|| Ok(false), || panic!("should leave Desktop closed"))
            .unwrap());
        assert!(!desktop.profile(undo.id).exists());
        original(&desktop);
        assert_eq!(fs::read(old_provider).unwrap(), old_bytes);
    }

    #[test]
    fn rollback_on_failed_launch_and_reject_broken_json_before_close() {
        let (_dir, desktop) = fixture();
        let tries = Cell::new(0);
        assert!(desktop
            .launch_with(
                ENDPOINT,
                KEY,
                || Ok(true),
                || {
                    tries.set(tries.get() + 1);
                    if tries.get() == 1 {
                        Err("activation failed".into())
                    } else {
                        original(&desktop);
                        Ok(())
                    }
                }
            )
            .is_err());
        assert_eq!(tries.get(), 2);
        original(&desktop);
        fs::write(desktop.path(File::Library), b"{invalid").unwrap();
        assert!(desktop
            .launch_with(
                ENDPOINT,
                KEY,
                || panic!("invalid config must not close Desktop"),
                || panic!("must not launch")
            )
            .is_err());
        assert_eq!(fs::read(desktop.path(File::Library)).unwrap(), b"{invalid");
    }

    #[test]
    fn upgrade_unchanged_owned_gateway_profile_to_official_api() {
        let (_dir, desktop) = fixture();
        let key = "sk-ant-bridge-fixture-local-only";
        desktop
            .launch_with(ENDPOINT, key, || Ok(false), || Ok(()))
            .unwrap();
        let (mut undo, _) = desktop.read_undo().unwrap().unwrap();
        let old_id = undo.id;
        let profile = serde_json::to_vec_pretty(&json!({
            "inferenceProvider": "gateway", "inferenceGatewayBaseUrl": ENDPOINT,
            "inferenceGatewayApiKey": "fixture-local-only", "inferenceGatewayAuthScheme": "bearer"
        }))
        .unwrap();
        undo.profile_hash = hash(&profile);
        let mut encrypted = document(Some(&profile)).unwrap();
        encrypted
            .apply(&Edit::set(
                &["inferenceGatewayApiKey"],
                "__sealed__:v1:legacy-local-key",
            ))
            .unwrap();
        fs::write(desktop.profile(old_id), encrypted.encode()).unwrap();
        let sealed = local_profile::protect(&serde_json::to_vec(&undo).unwrap(), true).unwrap();
        fs::write(desktop.record(), &sealed).unwrap();
        desktop
            .launch_with("http://127.0.0.1:52172", key, || Ok(false), || Ok(()))
            .unwrap();
        let (upgraded, _) = desktop.read_undo().unwrap().unwrap();
        assert_ne!(old_id, upgraded.id);
        let active = fields(&desktop.profile(upgraded.id)).unwrap();
        assert_eq!(active["inferenceProvider"], "anthropic");
        assert_eq!(active["inferenceCredentialKind"], "static");
        assert_eq!(active["egressProxyUrl"], "http://127.0.0.1:52172");
        assert!(active.get("inferenceGatewayBaseUrl").is_none());
        desktop.restore_with(|| Ok(false), || Ok(())).unwrap();
        original(&desktop);
    }

    #[test]
    fn desktop_sealed_local_key_can_be_reapplied_without_replacing_other_fields() {
        let (_dir, desktop) = fixture();
        desktop
            .launch_with(ENDPOINT, KEY, || Ok(false), || Ok(()))
            .unwrap();
        let (undo, _) = desktop.read_undo().unwrap().unwrap();
        let path = desktop.profile(undo.id);
        let mut doc = document(Some(&fs::read(&path).unwrap())).unwrap();
        doc.apply(&Edit::set(
            &["inferenceAnthropicApiKey"],
            "__sealed__:v1:fixture-opaque",
        ))
        .unwrap();
        doc.apply(&Edit::set(&["userPreference"], "keep me"))
            .unwrap();
        fs::write(&path, doc.encode()).unwrap();
        let closed = Cell::new(false);
        desktop
            .launch_with(
                ENDPOINT,
                KEY,
                || {
                    closed.set(true);
                    Ok(true)
                },
                || Ok(()),
            )
            .unwrap();
        assert!(closed.get());
        let applied = fields(&path).unwrap();
        assert_eq!(applied["inferenceAnthropicApiKey"], KEY);
        assert_eq!(applied["userPreference"], "keep me");
        assert!(desktop.restore_with(|| Ok(false), || Ok(())).unwrap());
        assert_eq!(fields(&path).unwrap()["userPreference"], "keep me");
    }

    #[test]
    fn restore_respects_a_new_provider_selection_and_preserves_recent_edits() {
        let (_dir, desktop) = fixture();
        desktop
            .launch_with(ENDPOINT, KEY, || Ok(false), || Ok(()))
            .unwrap();
        let path = desktop.path(File::Library);
        let mut edit = document(Some(&fs::read(&path).unwrap())).unwrap();
        edit.apply(&Edit {
            target: Target::Entry {
                path: vec!["entries".into()],
                id: "new-provider".into(),
            },
            value: Some(json!({"id":"new-provider", "name":"User edit"})),
        })
        .unwrap();
        edit.apply(&Edit::set(&["appliedId"], "new-provider"))
            .unwrap();
        fs::write(&path, edit.encode()).unwrap();
        assert!(desktop.restore_with(|| Ok(false), || Ok(())).unwrap());
        let meta = fields(&path).unwrap();
        assert_eq!(meta["appliedId"], "new-provider");
        assert_eq!(meta["entries"].as_array().unwrap().len(), 2);
        assert_eq!(
            fields(&desktop.path(File::Standard)).unwrap()["deploymentMode"],
            "3p"
        );
        assert!(!desktop.has_undo());
    }
}
