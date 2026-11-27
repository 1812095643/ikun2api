use crate::config_edit::atomic_write;
use anyhow::{ensure, Context, Result};
use serde::{Deserialize, Serialize};
use std::{
    path::{Path, PathBuf},
    process::{Command, Output},
};

pub fn default_root() -> Result<PathBuf> {
    Ok(dirs::data_local_dir()
        .context("无法确定应用数据目录")?
        .join("IKUNBridge"))
}
pub fn hidden(command: &mut Command) -> &mut Command {
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    command
}
pub fn output(command: &mut Command) -> Result<Output> {
    Ok(hidden(command).output()?)
}
pub fn find_cli() -> Option<PathBuf> {
    let name = if cfg!(windows) {
        "claude.exe"
    } else {
        "claude"
    };
    if let Some(home) = dirs::home_dir() {
        for p in [
            home.join(".local/bin").join(name),
            home.join(".claude/local").join(name),
        ] {
            if p.is_file() {
                return Some(p);
            }
        }
    }
    std::env::var_os("PATH").and_then(|paths| {
        std::env::split_paths(&paths)
            .map(|p| p.join(name))
            .find(|p| p.is_file())
    })
}
pub fn executable_running(program: &Path) -> Result<bool> {
    #[cfg(windows)]
    {
        use std::os::windows::ffi::OsStringExt;
        use windows_sys::Win32::{
            Foundation::{CloseHandle, HANDLE, INVALID_HANDLE_VALUE},
            System::{
                Diagnostics::ToolHelp::{
                    CreateToolhelp32Snapshot, Process32FirstW, Process32NextW, PROCESSENTRY32W,
                    TH32CS_SNAPPROCESS,
                },
                Threading::{
                    OpenProcess, QueryFullProcessImageNameW, PROCESS_QUERY_LIMITED_INFORMATION,
                },
            },
        };
        struct Handle(HANDLE);
        impl Drop for Handle {
            fn drop(&mut self) {
                unsafe {
                    CloseHandle(self.0);
                }
            }
        }
        let name = program
            .file_name()
            .context("无法识别客户端程序")?
            .to_string_lossy();
        let path = std::fs::canonicalize(program).unwrap_or_else(|_| program.to_owned());
        let path = path.to_string_lossy();
        let snapshot = unsafe { CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0) };
        if snapshot == INVALID_HANDLE_VALUE {
            return Err(std::io::Error::last_os_error().into());
        }
        let snapshot = Handle(snapshot);
        let mut entry = PROCESSENTRY32W {
            dwSize: std::mem::size_of::<PROCESSENTRY32W>() as u32,
            ..Default::default()
        };
        let mut has_entry = unsafe { Process32FirstW(snapshot.0, &mut entry) };
        let mut image = vec![0u16; 32768];
        while has_entry != 0 {
            let end = entry
                .szExeFile
                .iter()
                .position(|&c| c == 0)
                .unwrap_or(entry.szExeFile.len());
            if String::from_utf16_lossy(&entry.szExeFile[..end]).eq_ignore_ascii_case(&name) {
                let process = unsafe {
                    OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, entry.th32ProcessID)
                };
                if process.is_null() {
                    if std::io::Error::last_os_error().raw_os_error() != Some(87) {
                        return Ok(true);
                    }
                } else {
                    let process = Handle(process);
                    let mut len = image.len() as u32;
                    if unsafe {
                        QueryFullProcessImageNameW(process.0, 0, image.as_mut_ptr(), &mut len)
                    } == 0
                    {
                        return Ok(true);
                    }
                    let running = std::ffi::OsString::from_wide(&image[..len as usize]);
                    if running
                        .to_string_lossy()
                        .trim_start_matches("\\\\?\\")
                        .eq_ignore_ascii_case(path.trim_start_matches("\\\\?\\"))
                    {
                        return Ok(true);
                    }
                }
            }
            has_entry = unsafe { Process32NextW(snapshot.0, &mut entry) };
        }
        if std::io::Error::last_os_error().raw_os_error() == Some(18) {
            Ok(false)
        } else {
            Err(std::io::Error::last_os_error().into())
        }
    }
    #[cfg(target_os = "macos")]
    {
        let pattern = regex::escape(&program.display().to_string());
        Ok(output(Command::new("pgrep").args(["-f", &pattern]))?
            .status
            .success())
    }
    #[cfg(not(any(windows, target_os = "macos")))]
    {
        let _ = program;
        Ok(false)
    }
}

#[derive(Serialize, Deserialize)]
struct ElevationRequest {
    id: String,
    action: String,
    program: Option<PathBuf>,
    port: u16,
}
#[cfg(windows)]
fn elevated(action: &str, program: Option<&Path>, port: u16, root: &Path) -> Result<()> {
    use std::os::windows::ffi::OsStrExt;
    use windows_sys::Win32::UI::Shell::ShellExecuteW;
    let id = uuid::Uuid::new_v4().to_string();
    let request = ElevationRequest {
        id: id.clone(),
        action: action.into(),
        program: program.map(Path::to_owned),
        port,
    };
    let exe = std::env::current_exe()?;
    let args = format!(
        "--guard-helper {}",
        hex::encode(serde_json::to_vec(&request)?)
    );
    fn wide(s: &std::ffi::OsStr) -> Vec<u16> {
        s.encode_wide().chain(Some(0)).collect()
    }
    let verb = wide(std::ffi::OsStr::new("runas"));
    let exe = wide(exe.as_os_str());
    let args = wide(std::ffi::OsStr::new(&args));
    let result = unsafe {
        ShellExecuteW(
            std::ptr::null_mut(),
            verb.as_ptr(),
            exe.as_ptr(),
            args.as_ptr(),
            std::ptr::null(),
            0,
        )
    };
    ensure!(result as usize > 32, "系统权限请求未完成，未启动受管客户端");
    let result_file = root.join(format!("elevation-result-{id}.json"));
    for _ in 0..120 {
        if result_file.is_file() {
            let result: serde_json::Value = serde_json::from_slice(&std::fs::read(&result_file)?)?;
            std::fs::remove_file(result_file)?;
            ensure!(
                result["ok"] == true,
                "{}",
                result["error"].as_str().unwrap_or("出口规则操作失败")
            );
            return Ok(());
        }
        std::thread::sleep(std::time::Duration::from_millis(500));
    }
    anyhow::bail!("等待系统权限操作超时，客户端未启动")
}
pub fn guard_helper() -> Option<i32> {
    let args: Vec<_> = std::env::args().collect();
    if args.get(1).map(String::as_str) != Some("--guard-helper") {
        return None;
    }
    let result = (|| -> Result<()> {
        let request: ElevationRequest =
            serde_json::from_slice(&hex::decode(args.get(2).context("缺少规则请求")?)?)?;
        uuid::Uuid::parse_str(&request.id)?;
        let root = default_root()?;
        std::fs::create_dir_all(&root)?;
        let result = match request.action.as_str() {
            "install" => install_guard(
                &root,
                request.program.as_deref().context("缺少程序路径")?,
                request.port,
            )
            .map(|_| ()),
            "remove" => remove_guards(&root),
            _ => anyhow::bail!("规则请求无效"),
        };
        let value =
            serde_json::json!({"ok":result.is_ok(),"error":result.err().map(|e|e.to_string())});
        atomic_write(
            &root.join(format!("elevation-result-{}.json", request.id)),
            &serde_json::to_vec(&value)?,
        )?;
        Ok(())
    })();
    Some(if result.is_ok() { 0 } else { 1 })
}
pub fn ensure_guard(root: &Path, program: &Path, port: u16) -> Result<()> {
    if guard_present(root, program, port) {
        return Ok(());
    }
    ensure!(
        !executable_running(program)?,
        "此 CLI 已在运行，请先退出后再接管，避免影响现有任务"
    );
    if install_guard(root, program, port).is_err() {
        #[cfg(windows)]
        elevated("install", Some(program), port, root)?;
    }
    ensure!(
        guard_present(root, program, port),
        "出口规则未完成核验，客户端未启动"
    );
    Ok(())
}

pub fn ensure_idle(root: &Path) -> Result<()> {
    for rule in guards(root)? {
        ensure!(
            !executable_running(&rule.program)?,
            "请先退出受管 Claude Code 再撤销配置，现有会话未被关闭"
        );
    }
    Ok(())
}
pub fn check_cli_policy() -> Result<()> {
    #[cfg(windows)]
    {
        use windows_sys::Win32::System::Registry::{
            RegCloseKey, RegOpenKeyExW, HKEY_CURRENT_USER, HKEY_LOCAL_MACHINE, KEY_READ,
            KEY_WOW64_64KEY,
        };
        let key: Vec<u16> = "SOFTWARE\\Policies\\ClaudeCode"
            .encode_utf16()
            .chain(Some(0))
            .collect();
        for hive in [HKEY_LOCAL_MACHINE, HKEY_CURRENT_USER] {
            let mut opened = std::ptr::null_mut();
            let status = unsafe {
                RegOpenKeyExW(
                    hive,
                    key.as_ptr(),
                    0,
                    KEY_READ | KEY_WOW64_64KEY,
                    &mut opened,
                )
            };
            if status == 0 {
                unsafe {
                    RegCloseKey(opened);
                }
                anyhow::bail!("检测到 Claude Code 组织策略，此版本不接管受管客户端");
            }
            ensure!(
                status == 2 || status == 3,
                "无法核对 Claude Code 组织策略（{status}）"
            );
        }
        for name in ["ProgramW6432", "ProgramFiles", "ProgramFiles(x86)"] {
            if let Some(dir) = std::env::var_os(name) {
                let dir = PathBuf::from(dir).join("ClaudeCode");
                ensure!(
                    !dir.join("managed-settings.json").try_exists()?
                        && !dir.join("managed-settings.d").try_exists()?,
                    "检测到 Claude Code 组织策略，此版本不接管受管客户端"
                );
            }
        }
    }
    Ok(())
}
pub fn remove_guards_elevated(root: &Path) -> Result<()> {
    if let Err(_error) = remove_guards(root) {
        #[cfg(windows)]
        {
            elevated("remove", None, 0, root)?;
            return Ok(());
        }
        #[cfg(not(windows))]
        return Err(_error);
    }
    Ok(())
}
#[derive(Clone, Serialize)]
pub struct DesktopInstall {
    pub executable: PathBuf,
    pub config_root: PathBuf,
    pub version: String,
}
pub fn find_desktop() -> Result<Vec<DesktopInstall>> {
    let mut found = vec![];
    #[cfg(windows)]
    {
        let script="Get-AppxPackage -Name '*Claude*' | Select-Object InstallLocation,Version | ConvertTo-Json -Compress";
        let out = output(Command::new("powershell.exe").args([
            "-NoProfile",
            "-NonInteractive",
            "-Command",
            script,
        ]))?;
        if out.status.success() {
            if let Ok(v) = serde_json::from_slice::<serde_json::Value>(&out.stdout) {
                let a = match v {
                    serde_json::Value::Array(a) => a,
                    v => vec![v],
                };
                for p in a {
                    if let Some(root) = p["InstallLocation"].as_str() {
                        let exe = PathBuf::from(root).join("app/claude.exe");
                        if exe.exists() {
                            let local = dirs::data_local_dir().unwrap_or_default();
                            found.push(DesktopInstall {
                                executable: exe,
                                config_root: local.join("Claude-3p"),
                                version: p["Version"].as_str().unwrap_or("").into(),
                            });
                        }
                    }
                }
            }
        }
        if let Some(local) = dirs::data_local_dir() {
            for exe in [
                local.join("AnthropicClaude/Claude.exe"),
                local.join("Programs/Claude/Claude.exe"),
            ] {
                if exe.exists() {
                    found.push(DesktopInstall {
                        executable: exe,
                        config_root: local.join("Claude-3p"),
                        version: String::new(),
                    })
                }
            }
        }
    }
    #[cfg(target_os = "macos")]
    {
        for app in [
            PathBuf::from("/Applications/Claude.app"),
            dirs::home_dir()
                .unwrap_or_default()
                .join("Applications/Claude.app"),
        ] {
            let exe = app.join("Contents/MacOS/Claude");
            if exe.is_file() {
                found.push(DesktopInstall {
                    executable: exe,
                    config_root: dirs::home_dir()
                        .context("无法确定 home")?
                        .join("Library/Application Support/Claude-3p"),
                    version: String::new(),
                })
            }
        }
    }
    Ok(found)
}
#[derive(Clone, Serialize, Deserialize)]
pub struct GuardRecord {
    pub program: PathBuf,
    pub port: u16,
    pub keys: Vec<String>,
    pub active: bool,
}
#[cfg(windows)]
mod wfp {
    use super::*;
    use sha2::{Digest, Sha256};
    use std::{os::windows::ffi::OsStrExt, ptr};
    use windows_sys::{
        core::GUID,
        Win32::{
            Foundation::{LocalFree, HANDLE},
            NetworkManagement::WindowsFilteringPlatform::*,
            Security::Authorization::ConvertStringSecurityDescriptorToSecurityDescriptorW,
        },
    };
    const SUB: GUID = GUID::from_u128(0x0e232fee_d159_4403_9204_8bcb98f56271);
    fn wide(s: &std::ffi::OsStr) -> Vec<u16> {
        s.encode_wide().chain(Some(0)).collect()
    }
    fn check(code: u32) -> Result<()> {
        ensure!(
            code == 0,
            "Windows 出口规则操作失败（代码 {code}），需要管理员权限及 BFE 服务"
        );
        Ok(())
    }
    struct Engine(HANDLE);
    impl Drop for Engine {
        fn drop(&mut self) {
            unsafe {
                FwpmEngineClose0(self.0);
            }
        }
    }
    fn engine() -> Result<Engine> {
        let mut h = ptr::null_mut();
        check(unsafe { FwpmEngineOpen0(ptr::null(), 10, ptr::null(), ptr::null(), &mut h) })?;
        Ok(Engine(h))
    }
    fn guid(s: &str) -> Result<GUID> {
        Ok(GUID::from_u128(uuid::Uuid::parse_str(s)?.as_u128()))
    }
    fn identity() -> Result<String> {
        let r = output(Command::new("whoami.exe").args(["/user", "/fo", "csv", "/nh"]))?;
        ensure!(r.status.success(), "无法确定规则所属用户");
        let s = String::from_utf8_lossy(&r.stdout);
        let re = regex::Regex::new(r"S-1-[0-9-]+")?;
        Ok(re
            .find(&s)
            .context("当前用户 SID 不可用")?
            .as_str()
            .to_string())
    }
    pub fn install(program: &Path, port: u16, root: &Path) -> Result<GuardRecord> {
        ensure!(port >= 1024, "端口无效");
        let program = std::fs::canonicalize(program)?;
        let e = engine()?;
        let sid = identity()?;
        let mut name = wide(std::ffi::OsStr::new("IKUN Bridge · 本地出口"));
        let mut sub = FWPM_SUBLAYER0 {
            subLayerKey: SUB,
            flags: FWPM_SUBLAYER_FLAG_PERSISTENT,
            weight: 0x8000,
            ..Default::default()
        };
        sub.displayData.name = name.as_mut_ptr();
        let code = unsafe { FwpmSubLayerAdd0(e.0, &sub, ptr::null_mut()) };
        ensure!(
            code == 0 || code == 0x80320009,
            "创建出口规则组失败：{code}"
        );
        let filename = wide(program.as_os_str());
        let mut app = ptr::null_mut();
        check(unsafe { FwpmGetAppIdFromFileName0(filename.as_ptr(), &mut app) })?;
        struct App(*mut FWP_BYTE_BLOB);
        impl Drop for App {
            fn drop(&mut self) {
                unsafe { FwpmFreeMemory0(&mut self.0 as *mut _ as *mut *mut _) }
            }
        }
        let _app = App(app);
        let sddl = wide(std::ffi::OsStr::new(&format!("D:(A;;CC;;;{sid})")));
        let mut sd = ptr::null_mut();
        let mut sd_len = 0;
        ensure!(
            unsafe {
                ConvertStringSecurityDescriptorToSecurityDescriptorW(
                    sddl.as_ptr(),
                    1,
                    &mut sd,
                    &mut sd_len,
                )
            } != 0,
            "无法创建用户隔离条件"
        );
        struct Sd(*mut std::ffi::c_void);
        impl Drop for Sd {
            fn drop(&mut self) {
                unsafe {
                    LocalFree(self.0);
                }
            }
        }
        let _sd = Sd(sd);
        let mut blob = FWP_BYTE_BLOB {
            size: sd_len,
            data: sd as *mut u8,
        };
        let mut keys = vec![];
        for n in 0..3u8 {
            let mut hash = Sha256::new();
            hash.update(root.to_string_lossy().as_bytes());
            hash.update(program.to_string_lossy().to_lowercase().as_bytes());
            hash.update(sid.as_bytes());
            hash.update(port.to_le_bytes());
            hash.update([n]);
            let digest = hash.finalize();
            let mut bytes = [0; 16];
            bytes.copy_from_slice(&digest[..16]);
            keys.push(uuid::Uuid::from_bytes(bytes).to_string());
        }
        let previous = super::guards(root)?
            .into_iter()
            .filter(|r| r.program == program)
            .collect::<Vec<_>>();
        let record = GuardRecord {
            program,
            port,
            keys,
            active: false,
        };
        let path = root.join(format!(
            "guard-{}.json",
            hex::encode(Sha256::digest(
                filename
                    .iter()
                    .flat_map(|n| n.to_le_bytes())
                    .collect::<Vec<_>>()
            ))
        ));
        let mut pending = record.clone();
        for r in &previous {
            for key in &r.keys {
                if !pending.keys.contains(key) {
                    pending.keys.push(key.clone())
                }
            }
        }
        atomic_write(&path, &serde_json::to_vec(&pending)?)?;
        check(unsafe { FwpmTransactionBegin0(e.0, 0) })?;
        let result = (|| -> Result<()> {
            for r in &previous {
                for key in &r.keys {
                    let code = unsafe { FwpmFilterDeleteByKey0(e.0, &guid(key)?) };
                    ensure!(
                        code == 0 || code == 0x80320003,
                        "无法切换旧端口规则：{code}"
                    );
                }
            }
            for (n, key) in record.keys.iter().enumerate() {
                let key = guid(key)?;
                let del = unsafe { FwpmFilterDeleteByKey0(e.0, &key) };
                ensure!(del == 0 || del == 0x80320003, "无法替换已有出口规则：{del}");
                let mut conditions = vec![
                    FWPM_FILTER_CONDITION0 {
                        fieldKey: FWPM_CONDITION_ALE_APP_ID,
                        matchType: FWP_MATCH_EQUAL,
                        conditionValue: FWP_CONDITION_VALUE0 {
                            r#type: FWP_BYTE_BLOB_TYPE,
                            Anonymous: FWP_CONDITION_VALUE0_0 { byteBlob: app },
                        },
                    },
                    FWPM_FILTER_CONDITION0 {
                        fieldKey: FWPM_CONDITION_ALE_USER_ID,
                        matchType: FWP_MATCH_EQUAL,
                        conditionValue: FWP_CONDITION_VALUE0 {
                            r#type: FWP_SECURITY_DESCRIPTOR_TYPE,
                            Anonymous: FWP_CONDITION_VALUE0_0 { sd: &mut blob },
                        },
                    },
                ];
                if n == 0 {
                    conditions.push(FWPM_FILTER_CONDITION0 {
                        fieldKey: FWPM_CONDITION_IP_REMOTE_ADDRESS,
                        matchType: FWP_MATCH_NOT_EQUAL,
                        conditionValue: FWP_CONDITION_VALUE0 {
                            r#type: FWP_UINT32,
                            Anonymous: FWP_CONDITION_VALUE0_0 { uint32: 0x7f000001 },
                        },
                    });
                }
                if n == 1 {
                    conditions.push(FWPM_FILTER_CONDITION0 {
                        fieldKey: FWPM_CONDITION_IP_REMOTE_PORT,
                        matchType: FWP_MATCH_NOT_EQUAL,
                        conditionValue: FWP_CONDITION_VALUE0 {
                            r#type: FWP_UINT16,
                            Anonymous: FWP_CONDITION_VALUE0_0 { uint16: port },
                        },
                    });
                }
                let mut f = FWPM_FILTER0 {
                    filterKey: key,
                    ..Default::default()
                };
                f.displayData.name = name.as_mut_ptr();
                f.flags = FWPM_FILTER_FLAG_PERSISTENT;
                f.layerKey = if n == 2 {
                    FWPM_LAYER_ALE_AUTH_CONNECT_V6
                } else {
                    FWPM_LAYER_ALE_AUTH_CONNECT_V4
                };
                f.subLayerKey = SUB;
                f.weight = FWP_VALUE0 {
                    r#type: FWP_UINT8,
                    Anonymous: FWP_VALUE0_0 { uint8: 15 },
                };
                f.action.r#type = FWP_ACTION_BLOCK;
                f.numFilterConditions = conditions.len() as u32;
                f.filterCondition = conditions.as_mut_ptr();
                check(unsafe { FwpmFilterAdd0(e.0, &f, ptr::null_mut(), ptr::null_mut()) })?;
            }
            check(unsafe { FwpmTransactionCommit0(e.0) })?;
            Ok(())
        })();
        if result.is_err() {
            unsafe {
                FwpmTransactionAbort0(e.0);
            }
        }
        result?;
        let record = GuardRecord {
            active: true,
            ..record
        };
        atomic_write(&path, &serde_json::to_vec(&record)?)?;
        Ok(record)
    }
    pub fn remove(r: &GuardRecord) -> Result<()> {
        let e = engine()?;
        for key in &r.keys {
            let key = guid(key)?;
            let mut filter: *mut FWPM_FILTER0 = std::ptr::null_mut();
            let code = unsafe { FwpmFilterGetByKey0(e.0, &key, &mut filter) };
            if code == 0x80320003 {
                continue;
            }
            check(code)?;
            let owned = unsafe { filter.as_ref() }.is_some_and(|filter| {
                let key = filter.subLayerKey;
                key.data1 == SUB.data1
                    && key.data2 == SUB.data2
                    && key.data3 == SUB.data3
                    && key.data4 == SUB.data4
            });
            if !filter.is_null() {
                unsafe {
                    FwpmFreeMemory0(&mut filter as *mut _ as *mut *mut _);
                }
            }
            ensure!(owned, "拒绝移除不属于 IKUN Bridge 的出口规则");
            let code = unsafe { FwpmFilterDeleteByKey0(e.0, &key) };
            ensure!(code == 0 || code == 0x80320003, "移除出口规则失败：{code}");
        }
        Ok(())
    }
    pub fn present(r: &GuardRecord) -> bool {
        let Ok(e) = engine() else { return false };
        r.keys.iter().all(|key| {
            let Ok(key) = guid(key) else { return false };
            let mut f = ptr::null_mut();
            let code = unsafe { FwpmFilterGetByKey0(e.0, &key, &mut f) };
            if !f.is_null() {
                unsafe { FwpmFreeMemory0(&mut f as *mut _ as *mut *mut _) }
            }
            code == 0
        })
    }
}
pub fn guards(root: &Path) -> Result<Vec<GuardRecord>> {
    let mut result = vec![];
    for e in std::fs::read_dir(root)? {
        let p = e?.path();
        if p.file_name()
            .is_some_and(|x| x.to_string_lossy().starts_with("guard-"))
            && p.extension().is_some_and(|e| e == "json")
        {
            if let Ok(r) = serde_json::from_slice(&std::fs::read(&p)?) {
                result.push(r)
            }
        }
    }
    Ok(result)
}
pub fn guard_present(root: &Path, program: &Path, port: u16) -> bool {
    #[cfg(windows)]
    {
        let canonical = std::fs::canonicalize(program).ok();
        guards(root).unwrap_or_default().iter().any(|r| {
            Some(&r.program) == canonical.as_ref() && r.port == port && r.active && wfp::present(r)
        })
    }
    #[cfg(not(windows))]
    {
        let _ = (root, program, port);
        false
    }
}
pub fn install_guard(root: &Path, program: &Path, port: u16) -> Result<GuardRecord> {
    #[cfg(windows)]
    {
        wfp::install(program, port, root)
    }
    #[cfg(not(windows))]
    {
        let _ = (root, program, port);
        anyhow::bail!("此平台使用启动时进程沙箱，不安装 Windows 规则")
    }
}
pub fn remove_guards(root: &Path) -> Result<()> {
    for r in guards(root)? {
        #[cfg(windows)]
        wfp::remove(&r)?;
        let _ = r;
    }
    for e in std::fs::read_dir(root)? {
        let p = e?.path();
        if p.file_name()
            .is_some_and(|x| x.to_string_lossy().starts_with("guard-"))
            && p.extension().is_some_and(|e| e == "json")
        {
            std::fs::remove_file(p)?;
        }
    }
    Ok(())
}
pub fn install_user_certificate(root: &Path) -> Result<()> {
    let cert = root.join("bridge-ca.pem");
    ensure!(cert.is_file(), "请先初始化本地证书");
    // 查询系统实际信任，不以本地标记冒充成功；已信任时不重复修改 macOS 信任设置。
    if user_certificate_trusted(root)? {
        if !root.join("certificate-installed").exists() {
            atomic_write(&root.join("certificate-installed"), b"1")?;
        }
        return Ok(());
    }
    #[cfg(windows)]
    {
        let o = output(
            Command::new("certutil.exe")
                .args(["-user", "-addstore", "Root"])
                .arg(&cert),
        )?;
        ensure!(o.status.success(), "安装当前用户证书失败");
    }
    #[cfg(target_os = "macos")]
    {
        let keychain = dirs::home_dir()
            .context("home 不可用")?
            .join("Library/Keychains/login.keychain-db");
        let o = output(
            Command::new("security")
                .args(["add-trusted-cert", "-r", "trustRoot", "-k"])
                .arg(keychain)
                .arg(&cert),
        )?;
        ensure!(o.status.success(), "安装用户钥匙串证书失败");
    }
    ensure!(
        user_certificate_trusted(root)?,
        "证书授权尚未生效，请完成系统确认后重试"
    );
    atomic_write(&root.join("certificate-installed"), b"1")?;
    Ok(())
}

pub fn user_certificate_trusted(root: &Path) -> Result<bool> {
    let pem = std::fs::read(root.join("bridge-ca.pem")).context("无法读取本机证书")?;
    let der = rustls_pemfile::certs(&mut std::io::BufReader::new(&pem[..]))
        .next()
        .context("证书不可读")??;
    #[cfg(windows)]
    {
        use sha1::{Digest, Sha1};
        let hash = hex::encode(Sha1::digest(&der));
        let result = output(Command::new("certutil.exe").args(["-user", "-store", "Root", &hash]))?;
        Ok(result.status.success())
    }
    #[cfg(target_os = "macos")]
    {
        use security_framework::{certificate::SecCertificate, policy::SecPolicy, trust::SecTrust};
        let certificate = SecCertificate::from_der(&der)?;
        let mut trust =
            SecTrust::create_with_certificates(&[certificate], &[SecPolicy::create_x509()])?;
        trust.set_network_fetch_allowed(false)?;
        // 使用系统已有锚点离线验信任；不注入自定义锚点，也不调用任何写入或授权接口。
        Ok(trust.evaluate_with_error().is_ok())
    }
    #[cfg(not(any(windows, target_os = "macos")))]
    {
        let _ = der;
        anyhow::bail!("此平台暂不支持系统证书信任检查")
    }
}
pub fn remove_user_certificate(root: &Path) -> Result<()> {
    let p = root.join("bridge-ca.pem");
    if !root.join("certificate-installed").exists() {
        return Ok(());
    }
    let pem = std::fs::read(p)?;
    let der = rustls_pemfile::certs(&mut std::io::BufReader::new(&pem[..]))
        .next()
        .context("证书不可读")??;
    #[cfg(windows)]
    {
        use windows_sys::Win32::Security::Cryptography::{CryptHashCertificate, CALG_SHA1};
        let mut hash = [0u8; 20];
        let mut len = 20;
        ensure!(
            unsafe {
                CryptHashCertificate(
                    0,
                    CALG_SHA1,
                    0,
                    der.as_ptr(),
                    der.len() as u32,
                    hash.as_mut_ptr(),
                    &mut len,
                )
            } != 0,
            "计算证书指纹失败"
        );
        let out = output(Command::new("certutil.exe").args([
            "-user",
            "-delstore",
            "Root",
            &hex::encode(hash),
        ]))?;
        ensure!(out.status.success(), "移除本工具证书失败");
    }
    #[cfg(target_os = "macos")]
    {
        use sha1::{Digest, Sha1};
        let keychain = dirs::home_dir()
            .context("home 不可用")?
            .join("Library/Keychains/login.keychain-db");
        let hash = hex::encode(Sha1::digest(&der));
        let cert = root.join("bridge-ca.pem");
        let out = output(
            Command::new("security")
                .arg("remove-trusted-cert")
                .arg(&cert),
        )?;
        ensure!(out.status.success(), "移除本工具证书信任失败");
        let out = output(
            Command::new("security")
                .args(["delete-certificate", "-Z", &hash])
                .arg(&keychain),
        )?;
        ensure!(out.status.success(), "移除本工具钥匙串证书失败");
    }
    std::fs::remove_file(root.join("certificate-installed"))?;
    Ok(())
}
