use crate::config_edit::{atomic_write, FileRecord};
use anyhow::{ensure, Context, Result};
use rand::{rngs::OsRng, RngCore};
use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};

#[derive(Clone, Default, Serialize, Deserialize)]
pub struct Secrets {
    pub gateway_key: String,
    pub local_token: String,
    pub ca_key: String,
    pub ca_cert: String,
    pub leaf_key: String,
    pub leaf_cert: String,
}
impl Drop for Secrets {
    fn drop(&mut self) {
        use zeroize::Zeroize;
        self.gateway_key.zeroize();
        self.local_token.zeroize();
        self.ca_key.zeroize();
        self.leaf_key.zeroize();
    }
}
#[derive(Clone, Serialize, Deserialize)]
pub struct Journal {
    pub id: String,
    pub client: String,
    pub files: Vec<FileRecord>,
    pub pending: bool,
}

pub struct Store {
    pub root: PathBuf,
}
impl Store {
    pub fn new(root: PathBuf) -> Result<Self> {
        std::fs::create_dir_all(&root)?;
        private_directory(&root)?;
        Ok(Self { root })
    }
    pub fn secrets(&self) -> Result<Secrets> {
        let path = self.root.join("secrets.bin");
        if !path.exists() {
            let mut bytes = [0u8; 32];
            OsRng.fill_bytes(&mut bytes);
            let mut s = Secrets::default();
            s.local_token = format!("sk-ant-bridge-{}", hex::encode(bytes));
            self.save_secrets(&s)?;
            return Ok(s);
        }
        self.read_sealed(&path)
    }
    pub fn existing_secrets(&self) -> Result<Option<Secrets>> {
        let path = self.root.join("secrets.bin");
        if path.try_exists()? {
            Ok(Some(self.read_sealed(&path)?))
        } else {
            Ok(None)
        }
    }
    pub fn save_secrets(&self, s: &Secrets) -> Result<()> {
        self.write_sealed(&self.root.join("secrets.bin"), s)
    }
    pub fn write_sealed<T: Serialize>(&self, path: &Path, value: &T) -> Result<()> {
        let raw = serde_json::to_vec(value)?;
        let protected = protect(&raw, &self.root)?;
        atomic_write(path, &protected)
    }
    pub fn read_sealed<T: serde::de::DeserializeOwned>(&self, path: &Path) -> Result<T> {
        let raw = unprotect(&std::fs::read(path)?, &self.root)?;
        serde_json::from_slice(&raw).context("受保护数据无法读取，已停止配置修改")
    }
    pub fn journals(&self) -> Result<Vec<Journal>> {
        let folder = self.root.join("undo");
        if !folder.exists() {
            return Ok(vec![]);
        }
        let mut names = std::fs::read_dir(folder)?
            .filter_map(|e| e.ok())
            .map(|e| e.path())
            .filter(|p| p.extension().is_some_and(|e| e == "bin"))
            .collect::<Vec<_>>();
        names.sort();
        names.reverse();
        names.into_iter().map(|p| self.read_sealed(&p)).collect()
    }
    pub fn apply(&self, client: &str, files: Vec<FileRecord>) -> Result<String> {
        // Do not stack duplicate changes. A new launch may reuse the existing transaction.
        let files: Vec<_> = files
            .into_iter()
            .filter(|r| !r.changes.is_empty())
            .collect();
        if files.is_empty() {
            return Ok(String::new());
        }
        let folder = self.root.join("undo");
        std::fs::create_dir_all(&folder)?;
        private_directory(&folder)?;
        let id = format!(
            "{}-{}",
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)?
                .as_millis(),
            uuid::Uuid::new_v4()
        );
        let path = folder.join(format!("{id}.bin"));
        let mut j = Journal {
            id: id.clone(),
            client: client.into(),
            files,
            pending: true,
        };
        self.write_sealed(&path, &j)?;
        for (i, r) in j.files.iter().enumerate() {
            if let Err(e) = r.write() {
                for previous in j.files[..i].iter().rev() {
                    let _ = previous.rollback();
                }
                return Err(e.context("配置未全部成功，已尝试撤回本次写入；恢复记录已保留"));
            }
        }
        j.pending = false;
        self.write_sealed(&path, &j)?;
        Ok(id)
    }
    pub fn undo(&self, id: &str) -> Result<Vec<String>> {
        ensure!(
            id.chars().all(|c| c.is_ascii_hexdigit() || c == '-'),
            "恢复记录 ID 无效"
        );
        let path = self.root.join("undo").join(format!("{id}.bin"));
        let j: Journal = self.read_sealed(&path)?;
        let mut conflicts = vec![];
        for r in j.files.iter().rev() {
            conflicts.extend(r.rollback()?);
        }
        if conflicts.is_empty() {
            std::fs::remove_file(path)?
        }
        Ok(conflicts)
    }
}
pub fn private_directory(path: &Path) -> Result<()> {
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o700))?;
    }
    #[cfg(windows)]
    {
        // A new directory inherits the user's profile ACL; never grant broad principals.
        use std::os::windows::process::CommandExt;
        let user = std::env::var("USERNAME").context("无法识别当前用户")?;
        let output = std::process::Command::new("icacls.exe")
            .arg(path)
            .args([
                "/inheritance:r",
                "/grant:r",
                &format!("{user}:(OI)(CI)F"),
                "*S-1-5-18:(OI)(CI)F",
            ])
            .creation_flags(0x08000000)
            .output()?;
        ensure!(output.status.success(), "无法限制本地资料目录的访问权限");
    }
    Ok(())
}
#[cfg(windows)]
fn crypt(raw: &[u8], decrypt: bool) -> Result<Vec<u8>> {
    use windows_sys::Win32::{
        Foundation::LocalFree,
        Security::Cryptography::{
            CryptProtectData, CryptUnprotectData, CRYPTPROTECT_UI_FORBIDDEN, CRYPT_INTEGER_BLOB,
        },
    };
    let input = CRYPT_INTEGER_BLOB {
        cbData: raw.len().try_into()?,
        pbData: raw.as_ptr() as *mut u8,
    };
    let mut output = CRYPT_INTEGER_BLOB {
        cbData: 0,
        pbData: std::ptr::null_mut(),
    };
    let ok = unsafe {
        if decrypt {
            CryptUnprotectData(
                &input,
                std::ptr::null_mut(),
                std::ptr::null(),
                std::ptr::null(),
                std::ptr::null(),
                CRYPTPROTECT_UI_FORBIDDEN,
                &mut output,
            )
        } else {
            CryptProtectData(
                &input,
                std::ptr::null(),
                std::ptr::null(),
                std::ptr::null(),
                std::ptr::null(),
                CRYPTPROTECT_UI_FORBIDDEN,
                &mut output,
            )
        }
    };
    ensure!(
        ok != 0,
        "Windows DPAPI 操作失败：{}",
        std::io::Error::last_os_error()
    );
    let bytes =
        unsafe { std::slice::from_raw_parts(output.pbData, output.cbData as usize).to_vec() };
    unsafe { LocalFree(output.pbData as *mut _) };
    Ok(bytes)
}
#[cfg(windows)]
fn protect(raw: &[u8], _: &Path) -> Result<Vec<u8>> {
    let mut out = b"IKUN-DPAPI-1\0".to_vec();
    out.extend(crypt(raw, false)?);
    Ok(out)
}
#[cfg(windows)]
fn unprotect(raw: &[u8], _: &Path) -> Result<Vec<u8>> {
    let body = raw
        .strip_prefix(b"IKUN-DPAPI-1\0")
        .context("加密文件格式不支持")?;
    crypt(body, true)
}
#[cfg(target_os = "macos")]
fn key(root: &Path) -> Result<Vec<u8>> {
    use security_framework::passwords::{get_generic_password, set_generic_password};
    use sha2::{Digest, Sha256};
    let account = hex::encode(Sha256::digest(root.to_string_lossy().as_bytes()));
    match get_generic_password("com.ikun.claude-bridge", &account) {
        Ok(b) => Ok(b),
        Err(e) if e.code() == -25300 => {
            let mut b = vec![0; 32];
            OsRng.fill_bytes(&mut b);
            set_generic_password("com.ikun.claude-bridge", &account, &b)?;
            Ok(b)
        }
        Err(e) => Err(e.into()),
    }
}
#[cfg(target_os = "macos")]
fn protect(raw: &[u8], root: &Path) -> Result<Vec<u8>> {
    use aes_gcm::{aead::Aead, Aes256Gcm, KeyInit, Nonce};
    let k = key(root)?;
    let c = Aes256Gcm::new_from_slice(&k).map_err(|_| anyhow::anyhow!("钥匙串密钥格式不正确"))?;
    let mut n = [0; 12];
    OsRng.fill_bytes(&mut n);
    let mut out = b"IKUN-KEYCHAIN-1\0".to_vec();
    out.extend(n);
    out.extend(
        c.encrypt(Nonce::from_slice(&n), raw)
            .map_err(|_| anyhow::anyhow!("本地加密失败"))?,
    );
    Ok(out)
}
#[cfg(target_os = "macos")]
fn unprotect(raw: &[u8], root: &Path) -> Result<Vec<u8>> {
    use aes_gcm::{aead::Aead, Aes256Gcm, KeyInit, Nonce};
    let data = raw
        .strip_prefix(b"IKUN-KEYCHAIN-1\0")
        .context("加密文件格式不支持")?;
    ensure!(data.len() > 12, "加密文件不完整");
    let k = key(root)?;
    let c = Aes256Gcm::new_from_slice(&k).map_err(|_| anyhow::anyhow!("钥匙串密钥格式不正确"))?;
    c.decrypt(Nonce::from_slice(&data[..12]), &data[12..])
        .map_err(|_| anyhow::anyhow!("本地解密失败"))
}
#[cfg(not(any(windows, target_os = "macos")))]
fn protect(_: &[u8], _: &Path) -> Result<Vec<u8>> {
    anyhow::bail!("仅支持 Windows 与 macOS 的系统密钥保护")
}
#[cfg(not(any(windows, target_os = "macos")))]
fn unprotect(_: &[u8], _: &Path) -> Result<Vec<u8>> {
    anyhow::bail!("仅支持 Windows 与 macOS 的系统密钥保护")
}
