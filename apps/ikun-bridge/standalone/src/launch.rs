use std::{
    path::{Path, PathBuf},
    process::Command,
};

fn find_cli(path: &str) -> Result<PathBuf, String> {
    let name = if cfg!(windows) {
        "claude.exe"
    } else {
        "claude"
    };
    let path = path.trim();
    let candidate = if path.is_empty() {
        let mut paths = vec![];
        if let Some(home) = std::env::var_os(if cfg!(windows) { "USERPROFILE" } else { "HOME" }) {
            paths.push(PathBuf::from(home).join(".local").join("bin"));
        }
        if let Some(path) = std::env::var_os("PATH") {
            paths.extend(std::env::split_paths(&path));
        }
        paths
            .into_iter()
            .filter(|dir| dir.is_absolute())
            .map(|dir| dir.join(name))
            .find(|p| p.is_file())
    } else {
        let p = Path::new(path);
        if !p.is_absolute()
            || !p
                .file_name()
                .is_some_and(|n| n.to_string_lossy().eq_ignore_ascii_case(name))
        {
            return Err(format!("请选择 {name} 的绝对路径"));
        }
        Some(p.to_owned()).filter(|p| p.is_file())
    };
    candidate.ok_or_else(|| format!("未找到 {name}"))
}

pub fn has_cli(path: &str) -> bool {
    find_cli(path).is_ok()
}

#[cfg(windows)]
fn desktop_package_exists() -> bool {
    use windows::{
        core::w,
        Win32::{
            Foundation::ERROR_INSUFFICIENT_BUFFER,
            Storage::Packaging::Appx::GetPackagesByPackageFamily,
        },
    };
    let (mut count, mut len) = (0, 0);
    let result = unsafe {
        GetPackagesByPackageFamily(w!("Claude_pzs8sxrjxfjjc"), &mut count, None, &mut len, None)
    };
    result == ERROR_INSUFFICIENT_BUFFER && count > 0
}

#[cfg(windows)]
fn activate_desktop_package() -> Result<(), String> {
    use windows::{
        core::{w, PCWSTR},
        Win32::{
            Foundation::RPC_E_CHANGED_MODE,
            System::Com::{
                CoCreateInstance, CoInitializeEx, CoUninitialize, CLSCTX_LOCAL_SERVER,
                COINIT_APARTMENTTHREADED,
            },
            UI::Shell::{
                ApplicationActivationManager, IApplicationActivationManager, AO_NOERRORUI,
            },
        },
    };
    // COM initialization and release must happen on the same thread.
    let initialized = unsafe { CoInitializeEx(None, COINIT_APARTMENTTHREADED) };
    if initialized.is_err() && initialized != RPC_E_CHANGED_MODE {
        return Err("无法初始化 Windows 应用启动器".into());
    }
    let result = unsafe {
        CoCreateInstance::<_, IApplicationActivationManager>(
            &ApplicationActivationManager,
            None,
            CLSCTX_LOCAL_SERVER,
        )
        .and_then(|manager| {
            manager.ActivateApplication(
                w!("Claude_pzs8sxrjxfjjc!Claude"),
                PCWSTR::null(),
                AO_NOERRORUI,
            )
        })
    };
    if initialized.is_ok() {
        unsafe { CoUninitialize() };
    }
    result
        .map(|_| ())
        .map_err(|e| format!("无法启动 Claude Desktop（{}）", e.code()))
}

#[cfg(windows)]
fn desktop_path() -> Option<PathBuf> {
    let local = PathBuf::from(std::env::var_os("LOCALAPPDATA")?);
    ["AnthropicClaude/Claude.exe", "Programs/Claude/Claude.exe"]
        .into_iter()
        .map(|path| local.join(path))
        .find(|path| path.is_absolute() && path.is_file())
}

#[cfg(target_os = "macos")]
fn desktop_path() -> Option<PathBuf> {
    std::iter::once(PathBuf::from("/Applications/Claude.app"))
        .chain(
            std::env::var_os("HOME")
                .map(|home| PathBuf::from(home).join("Applications/Claude.app")),
        )
        .find(|app| app.is_absolute() && app.join("Contents/MacOS/Claude").is_file())
}

#[cfg(not(any(windows, target_os = "macos")))]
fn desktop_path() -> Option<PathBuf> {
    None
}

pub fn has_desktop() -> bool {
    #[cfg(windows)]
    if desktop_package_exists() {
        return true;
    }
    desktop_path().is_some()
}

pub fn desktop() -> Result<(), String> {
    #[cfg(windows)]
    if desktop_package_exists() {
        return activate_desktop_package();
    }
    let path = desktop_path().ok_or("未找到 Claude Desktop")?;
    #[cfg(target_os = "macos")]
    let result = Command::new("/usr/bin/open")
        .arg(path)
        .status()
        .map(|status| status.success());
    #[cfg(not(target_os = "macos"))]
    let result = Command::new(path).spawn().map(|_| true);
    match result {
        Ok(true) => Ok(()),
        _ => Err("无法启动 Claude Desktop".into()),
    }
}

pub fn check_desktop_policy() -> anyhow::Result<()> {
    use anyhow::{bail, ensure};
    ensure!(has_desktop(), "未找到 Claude Desktop");
    #[cfg(windows)]
    {
        use windows::{
            core::w,
            Win32::{
                Foundation::{ERROR_FILE_NOT_FOUND, ERROR_PATH_NOT_FOUND, ERROR_SUCCESS},
                System::Registry::{
                    RegCloseKey, RegOpenKeyExW, HKEY, HKEY_CURRENT_USER, HKEY_LOCAL_MACHINE,
                    KEY_READ, KEY_WOW64_64KEY,
                },
            },
        };
        for hive in [HKEY_LOCAL_MACHINE, HKEY_CURRENT_USER] {
            let mut key = HKEY::default();
            match unsafe {
                RegOpenKeyExW(
                    hive,
                    w!("SOFTWARE\\Policies\\Claude"),
                    None,
                    KEY_READ | KEY_WOW64_64KEY,
                    &mut key,
                )
            } {
                ERROR_SUCCESS => {
                    let _ = unsafe { RegCloseKey(key) };
                    bail!("Claude Desktop 由系统策略管理，无法自动切换");
                }
                ERROR_FILE_NOT_FOUND | ERROR_PATH_NOT_FOUND => {}
                _ => bail!("无法检查 Claude Desktop 系统策略"),
            }
        }
    }
    #[cfg(target_os = "macos")]
    {
        let root = Path::new("/Library/Managed Preferences");
        let name = "com.anthropic.claudefordesktop.plist";
        let mut paths = vec![root.join(name)];
        if let Some(user) =
            std::env::var_os("USER").filter(|user| Path::new(user).components().count() == 1)
        {
            paths.push(root.join(user).join(name));
        }
        for path in paths {
            match std::fs::symlink_metadata(path) {
                Ok(_) => bail!("Claude Desktop 由系统策略管理，无法自动切换"),
                Err(error) if error.kind() == std::io::ErrorKind::NotFound => {}
                Err(_) => bail!("无法检查 Claude Desktop 系统策略"),
            }
        }
    }
    Ok(())
}

#[cfg(windows)]
fn desktop_processes() -> anyhow::Result<Vec<u32>> {
    use windows::{
        core::PWSTR,
        Win32::{
            Foundation::{CloseHandle, ERROR_NO_MORE_FILES, ERROR_SUCCESS, HANDLE},
            Storage::Packaging::Appx::GetPackageFamilyName,
            System::{
                Diagnostics::ToolHelp::{
                    CreateToolhelp32Snapshot, Process32FirstW, Process32NextW, PROCESSENTRY32W,
                    TH32CS_SNAPPROCESS,
                },
                RemoteDesktop::ProcessIdToSessionId,
                Threading::{
                    OpenProcess, QueryFullProcessImageNameW, PROCESS_NAME_WIN32,
                    PROCESS_QUERY_LIMITED_INFORMATION,
                },
            },
        },
    };
    struct Handle(HANDLE);
    impl Drop for Handle {
        fn drop(&mut self) {
            let _ = unsafe { CloseHandle(self.0) };
        }
    }
    let legacy = desktop_path().map(|p| p.to_string_lossy().to_ascii_lowercase());
    let mut session = 0;
    unsafe { ProcessIdToSessionId(std::process::id(), &mut session) }?;
    let snapshot = Handle(unsafe { CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0) }?);
    let mut entry = PROCESSENTRY32W {
        dwSize: std::mem::size_of::<PROCESSENTRY32W>() as u32,
        ..Default::default()
    };
    unsafe { Process32FirstW(snapshot.0, &mut entry) }?;
    let mut matches = vec![];
    loop {
        let name = String::from_utf16_lossy(
            &entry.szExeFile[..entry
                .szExeFile
                .iter()
                .position(|&n| n == 0)
                .unwrap_or(entry.szExeFile.len())],
        );
        let pid = entry.th32ProcessID;
        let mut other_session = 0;
        if name.eq_ignore_ascii_case("Claude.exe")
            && unsafe { ProcessIdToSessionId(pid, &mut other_session) }.is_ok()
            && other_session == session
        {
            if let Ok(handle) =
                unsafe { OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, pid) }
            {
                let process = Handle(handle);
                let mut family = [0u16; 128];
                let mut family_len = family.len() as u32;
                let packaged = unsafe {
                    GetPackageFamilyName(
                        process.0,
                        &mut family_len,
                        Some(PWSTR(family.as_mut_ptr())),
                    )
                } == ERROR_SUCCESS
                    && String::from_utf16_lossy(
                        &family[..(family_len.saturating_sub(1) as usize).min(family.len())],
                    )
                    .eq_ignore_ascii_case("Claude_pzs8sxrjxfjjc");
                let mut path = vec![0u16; 32768];
                let mut len = path.len() as u32;
                if unsafe {
                    QueryFullProcessImageNameW(
                        process.0,
                        PROCESS_NAME_WIN32,
                        PWSTR(path.as_mut_ptr()),
                        &mut len,
                    )
                }
                .is_ok()
                {
                    let image =
                        String::from_utf16_lossy(&path[..len as usize]).to_ascii_lowercase();
                    if (packaged && image.ends_with("\\app\\claude.exe"))
                        || legacy.as_ref() == Some(&image)
                    {
                        matches.push((pid, entry.th32ParentProcessID));
                    }
                }
            }
        }
        match unsafe { Process32NextW(snapshot.0, &mut entry) } {
            Ok(()) => {}
            Err(error) if error.code() == ERROR_NO_MORE_FILES.to_hresult() => break,
            Err(error) => return Err(error.into()),
        }
    }
    Ok(matches
        .iter()
        .filter(|(_, parent)| !matches.iter().any(|(pid, _)| pid == parent))
        .map(|(pid, _)| *pid)
        .collect())
}

#[cfg(windows)]
pub fn close_desktop() -> anyhow::Result<bool> {
    use anyhow::{anyhow, ensure};
    use windows::{
        core::BOOL,
        Win32::{
            Foundation::{HWND, LPARAM, WPARAM},
            UI::WindowsAndMessaging::{
                EnumWindows, GetWindowThreadProcessId, IsWindowVisible, PostMessageW, WM_CLOSE,
            },
        },
    };
    unsafe extern "system" fn request_close(window: HWND, arg: LPARAM) -> BOOL {
        let pids = unsafe { &*(arg.0 as *const Vec<u32>) };
        let mut pid = 0;
        unsafe {
            GetWindowThreadProcessId(window, Some(&mut pid));
            if pids.contains(&pid) && IsWindowVisible(window).as_bool() {
                let _ = PostMessageW(Some(window), WM_CLOSE, WPARAM(0), LPARAM(0));
            }
        }
        true.into()
    }
    let check = || desktop_processes().map_err(|_| anyhow!("无法检查 Claude Desktop 运行状态"));
    let pids = check()?;
    if pids.is_empty() {
        return Ok(false);
    }
    unsafe {
        EnumWindows(
            Some(request_close),
            LPARAM(&pids as *const Vec<u32> as isize),
        )
    }
    .map_err(|_| anyhow!("无法请求 Claude Desktop 退出"))?;
    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(8);
    loop {
        if check()?.is_empty() {
            return Ok(true);
        }
        ensure!(
            std::time::Instant::now() < deadline,
            "Claude Desktop 仍在运行，请完全退出后重试"
        );
        std::thread::sleep(std::time::Duration::from_millis(150));
    }
}

#[cfg(target_os = "macos")]
pub fn close_desktop() -> anyhow::Result<bool> {
    use anyhow::ensure;
    use objc2::rc::autoreleasepool;
    use objc2_app_kit::NSRunningApplication;
    use objc2_foundation::NSString;
    let count = |request| {
        autoreleasepool(|_| {
            let id = NSString::from_str("com.anthropic.claudefordesktop");
            let apps = NSRunningApplication::runningApplicationsWithBundleIdentifier(&id);
            let mut running = 0;
            for index in 0..apps.count() {
                let app = apps.objectAtIndex(index);
                if app.isTerminated() {
                    continue;
                }
                running += 1;
                if request {
                    app.terminate();
                }
            }
            running
        })
    };
    if count(true) == 0 {
        return Ok(false);
    }
    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(8);
    loop {
        if count(false) == 0 {
            return Ok(true);
        }
        ensure!(
            std::time::Instant::now() < deadline,
            "Claude Desktop 仍在运行，请完全退出后重试"
        );
        std::thread::sleep(std::time::Duration::from_millis(150));
    }
}

#[cfg(not(any(windows, target_os = "macos")))]
pub fn close_desktop() -> anyhow::Result<bool> {
    anyhow::bail!("此平台暂不支持 Claude Desktop")
}

pub fn cli(path: &str, proxy: &str, local_key: &str, ca_file: &Path) -> Result<(), String> {
    let path = find_cli(path)?;
    #[cfg(windows)]
    let result = {
        use std::os::windows::process::CommandExt;
        Command::new(path)
            .env("ANTHROPIC_BASE_URL", "https://api.anthropic.com")
            .env("ANTHROPIC_API_KEY", local_key)
            .env_remove("ANTHROPIC_AUTH_TOKEN")
            .env("HTTPS_PROXY", proxy)
            .env("HTTP_PROXY", proxy)
            .env("https_proxy", proxy)
            .env("http_proxy", proxy)
            .env("ALL_PROXY", proxy)
            .env("all_proxy", proxy)
            .env("NODE_EXTRA_CA_CERTS", ca_file)
            .env_remove("NODE_TLS_REJECT_UNAUTHORIZED")
            .env("DISABLE_AUTOUPDATER", "1")
            .env("DISABLE_ERROR_REPORTING", "1")
            .env("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1")
            .env("CLAUDE_CODE_ENABLE_TELEMETRY", "0")
            .env("NO_PROXY", no_proxy())
            .env("no_proxy", no_proxy())
            .creation_flags(0x0000_0010)
            .spawn()
    };
    #[cfg(target_os = "macos")]
    {
        // Use Terminal for the interactive TTY when opened from Finder.
        // Apple's quoted form handles every argument as a literal shell word.
        let script = r#"on run argv
  set commandLine to "env -u ANTHROPIC_AUTH_TOKEN -u NODE_TLS_REJECT_UNAUTHORIZED CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 DISABLE_AUTOUPDATER=1 DISABLE_TELEMETRY=1 DISABLE_ERROR_REPORTING=1 CLAUDE_CODE_ENABLE_TELEMETRY=0 ANTHROPIC_BASE_URL=https://api.anthropic.com ANTHROPIC_API_KEY=" & quoted form of item 1 of argv & " HTTPS_PROXY=" & quoted form of item 2 of argv & " HTTP_PROXY=" & quoted form of item 2 of argv & " https_proxy=" & quoted form of item 2 of argv & " http_proxy=" & quoted form of item 2 of argv & " ALL_PROXY=" & quoted form of item 2 of argv & " all_proxy=" & quoted form of item 2 of argv & " NODE_EXTRA_CA_CERTS=" & quoted form of item 3 of argv & " NO_PROXY=localhost,127.0.0.1,::1 no_proxy=localhost,127.0.0.1,::1 " & quoted form of item 4 of argv
  tell application "Terminal"
    activate
    do script commandLine
  end tell
end run"#;
        return match Command::new("/usr/bin/osascript")
            .args(["-e", script, "--", local_key, proxy])
            .arg(ca_file)
            .arg(path)
            .output()
        {
            Ok(output) if output.status.success() => Ok(()),
            _ => Err("无法在终端中启动 Claude Code".into()),
        };
    };
    #[cfg(all(not(windows), not(target_os = "macos")))]
    let result = Command::new(path)
        .env("ANTHROPIC_BASE_URL", "https://api.anthropic.com")
        .env("ANTHROPIC_API_KEY", local_key)
        .env_remove("ANTHROPIC_AUTH_TOKEN")
        .env("HTTPS_PROXY", proxy)
        .env("HTTP_PROXY", proxy)
        .env("https_proxy", proxy)
        .env("http_proxy", proxy)
        .env("NODE_EXTRA_CA_CERTS", ca_file)
        .env("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1")
        .env("CLAUDE_CODE_ENABLE_TELEMETRY", "0")
        .env("NO_PROXY", no_proxy())
        .env("no_proxy", no_proxy())
        .spawn();
    #[cfg(not(target_os = "macos"))]
    result
        .map(|_| ())
        .map_err(|_| "无法启动 Claude Code".into())
}

#[cfg(not(target_os = "macos"))]
fn no_proxy() -> String {
    "localhost,127.0.0.1,::1".into()
}
