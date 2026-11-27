#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

#[allow(dead_code)]
#[path = "../../src-tauri/src/config_edit.rs"]
mod config_edit;
mod desktop_profile;
mod gateway;
mod launch;
mod local_profile;
mod profiles;
mod protection;
#[cfg(test)]
mod tests;

use axum::{
    body::{Body, Bytes},
    extract::{DefaultBodyLimit, State},
    http::{header, HeaderMap, Method, Request, StatusCode, Uri},
    middleware::{self, Next},
    response::Response,
    routing::{get, post},
    Router,
};
use gateway::{error, Gateway};
use serde::Serialize;
use std::{
    sync::{
        atomic::{AtomicBool, AtomicU64, Ordering},
        Arc, RwLock,
    },
    time::Duration,
};
use subtle::ConstantTimeEq;
use tauri::{Manager, State as AppState};
use tokio::{net::TcpListener, sync::Mutex};
use tokio_util::sync::CancellationToken;
use zeroize::Zeroizing;

const MAX_BODY: usize = 64 * 1024 * 1024;

#[derive(Default)]
struct BridgeState(Mutex<Option<LocalBridge>>);

#[derive(Default)]
struct ExitIntent(AtomicBool);

#[derive(Default)]
struct ProfilesLock(std::sync::Mutex<()>);

struct LocalBridge {
    connection: Arc<Connection>,
    endpoint: String,
    protection: Option<Arc<protection::Protection>>,
    profile_id: Option<String>,
}

struct Connection {
    host: String,
    gateway: RwLock<Arc<Gateway>>,
    client_key: Zeroizing<String>,
    desktop_key: Zeroizing<String>,
    requests: AtomicU64,
    cli_configured: AtomicBool,
    desktop_configured: AtomicBool,
    desktop_trust: AtomicBool,
    shutdown: CancellationToken,
}

impl Connection {
    fn gateway(&self) -> Arc<Gateway> {
        self.gateway
            .read()
            .unwrap_or_else(|error| error.into_inner())
            .clone()
    }

    fn switch_gateway(&self, gateway: Gateway) {
        // 仅替换后续请求的网关；进行中的请求保留原会话，本地代理端口和客户端配置不变。
        *self
            .gateway
            .write()
            .unwrap_or_else(|error| error.into_inner()) = Arc::new(gateway);
    }
}

impl Drop for LocalBridge {
    fn drop(&mut self) {
        self.connection.shutdown.cancel();
        if let Some(protection) = &self.protection {
            protection.proxy.cancel.cancel();
        }
    }
}

impl LocalBridge {
    fn serve(
        listener: TcpListener,
        gateway: Gateway,
        desktop_key: Zeroizing<String>,
    ) -> std::io::Result<Self> {
        let host = listener.local_addr()?.to_string();
        let endpoint = format!("http://{host}");
        let connection = Arc::new(Connection {
            host,
            gateway: RwLock::new(Arc::new(gateway)),
            client_key: Zeroizing::new(local_profile::new_key()),
            desktop_key,
            requests: AtomicU64::new(0),
            cli_configured: AtomicBool::new(false),
            desktop_configured: AtomicBool::new(false),
            desktop_trust: AtomicBool::new(false),
            shutdown: CancellationToken::new(),
        });
        let shutdown = connection.shutdown.clone();
        let service = routes(connection.clone());
        tauri::async_runtime::spawn(async move {
            let _ = axum::serve(listener, service)
                .with_graceful_shutdown(shutdown.cancelled_owned())
                .await;
        });
        let renew = connection.clone();
        tauri::async_runtime::spawn(async move {
            let refresh = async {
                let mut tick = tokio::time::interval(Duration::from_secs(4));
                loop {
                    tick.tick().await;
                    let _ = renew.gateway().refresh(false).await;
                }
            };
            tokio::select! { _ = renew.shutdown.cancelled() => {}, _ = refresh => {} }
        });
        Ok(Self {
            connection,
            endpoint,
            protection: None,
            profile_id: None,
        })
    }
}

#[derive(Serialize)]
struct Status {
    running: bool,
    endpoint: Option<String>,
    ticket: Option<String>,
    reason: Option<String>,
    requests: u64,
    version: &'static str,
    gateway: Option<String>,
    profile_id: Option<String>,
    client_endpoint: &'static str,
    actions: Vec<OperationStatus>,
}

#[derive(Serialize)]
struct OperationStatus {
    label: &'static str,
    state: &'static str,
    detail: String,
}

#[tauri::command]
async fn bridge_status(state: AppState<'_, BridgeState>) -> Result<Status, ()> {
    let bridge = state.0.lock().await.as_ref().map(|value| {
        (
            value.connection.clone(),
            value.endpoint.clone(),
            value.protection.clone(),
            value.profile_id.clone(),
        )
    });
    let mut status = Status {
        running: false,
        endpoint: None,
        ticket: None,
        reason: None,
        requests: 0,
        version: env!("CARGO_PKG_VERSION"),
        gateway: None,
        profile_id: None,
        client_endpoint: "https://api.anthropic.com",
        actions: vec![OperationStatus {
            label: "本地代理",
            state: "pending",
            detail: "等待启动".into(),
        }],
    };
    if let Some((connection, endpoint, protection, profile_id)) = bridge {
        let gateway = connection.gateway();
        let (label, reason) = gateway.status().await;
        status.running = true;
        status.profile_id = profile_id;
        status.gateway = Some(gateway.base.as_str().trim_end_matches('/').to_owned());
        status.endpoint = Some(protection.as_ref().map_or(endpoint, |p| p.url()));
        status.ticket = Some(label);
        status.reason = reason;
        status.requests = connection.requests.load(Ordering::Relaxed);
        status.actions.clear();
        status.actions.push(OperationStatus {
            label: "上游网关",
            state: "done",
            detail: status.gateway.clone().unwrap_or_default(),
        });
        let verified = gateway.admission().await;
        status.actions.push(OperationStatus {
            label: "通行证校验",
            state: if verified.0 { "done" } else { "pending" },
            detail: verified.1,
        });
        if let Some(protection) = protection {
            let metrics = protection.proxy.metrics.snapshot();
            status.actions.push(OperationStatus {
                label: "官方 API 代理",
                state: if protection.proxy.alive() {
                    "done"
                } else {
                    "error"
                },
                detail: "客户端入口 https://api.anthropic.com".into(),
            });
            status.actions.push(OperationStatus {
                label: "隐私过滤",
                state: "done",
                detail: format!(
                    "已过滤 {} 次 · 已拦截 {} 次非放行请求",
                    metrics.filtered, metrics.blocked
                ),
            });
            let cli = connection.cli_configured.load(Ordering::Relaxed);
            let desktop = connection.desktop_configured.load(Ordering::Relaxed);
            for (label, applied) in [("Claude Code 配置", cli), ("Desktop 配置", desktop)] {
                status.actions.push(OperationStatus {
                    label,
                    state: if applied { "done" } else { "pending" },
                    detail: if applied {
                        "官方 API Key 入口已配置，更新与遥测关闭项已写入".into()
                    } else {
                        "等待从 Bridge 启动客户端".into()
                    },
                });
            }
            let targets = match (cli, desktop) {
                (true, true) => "Claude Code / Desktop",
                (true, false) => "Claude Code",
                (false, true) => "Desktop",
                _ => "",
            };
            for label in ["自动更新", "遥测与附加服务"] {
                status.actions.push(OperationStatus {
                    label,
                    state: if targets.is_empty() {
                        "pending"
                    } else {
                        "done"
                    },
                    detail: if targets.is_empty() {
                        "等待配置客户端".into()
                    } else {
                        format!("已在 {targets} 写入关闭项")
                    },
                });
            }
            let trusted = connection.desktop_trust.load(Ordering::Relaxed);
            status.actions.push(OperationStatus {
                label: "Desktop 证书",
                state: if trusted { "done" } else { "pending" },
                detail: if trusted {
                    "本工具的当前用户证书信任已安装".into()
                } else {
                    "首次启动 Desktop 时配置".into()
                },
            });
            status.actions.push(OperationStatus {
                label: "请求接管",
                state: if status.requests > 0 {
                    "done"
                } else {
                    "pending"
                },
                detail: if status.requests > 0 {
                    format!("已收到 {} 次客户端请求", status.requests)
                } else {
                    "尚未观察到客户端请求，领证成功不代表请求已接管".into()
                },
            });
        }
    }
    Ok(status)
}

#[tauri::command]
async fn bridge_start(
    app: tauri::AppHandle,
    state: AppState<'_, BridgeState>,
    profile_id: String,
) -> Result<(), String> {
    let mut current = state.0.lock().await;
    let id = profile_id.clone();
    let profile = profile_task(app.clone(), move |repo| repo.get(&id)).await?;
    let gateway = Gateway::new(&profile.gateway, profile.key.clone())?;
    gateway.refresh(true).await?;
    gateway.verify_access().await?;
    let id = profile_id.clone();
    profile_task(app.clone(), move |repo| repo.select(&id)).await?;
    if let Some(bridge) = current.as_mut() {
        bridge.connection.switch_gateway(gateway);
        bridge.profile_id = Some(profile_id);
        return Ok(());
    }
    let root = app
        .path()
        .app_local_data_dir()
        .map_err(|_| "无法找到 Bridge 配置目录")?;
    let (listener, desktop_key) = local_profile::bind(&root)?;
    let mut bridge =
        LocalBridge::serve(listener, gateway, desktop_key).map_err(|_| "无法启动本地服务")?;
    bridge.protection = Some(Arc::new(
        protection::Protection::start(&root, &bridge.endpoint, &bridge.connection.desktop_key)
            .await
            .map_err(|e| e.to_string())?,
    ));
    bridge.profile_id = Some(profile_id);
    *current = Some(bridge);
    Ok(())
}

async fn profile_task<T: Send + 'static>(
    app: tauri::AppHandle,
    task: impl FnOnce(&mut profiles::Repository) -> anyhow::Result<T> + Send + 'static,
) -> Result<T, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let lock = app.state::<ProfilesLock>();
        let _guard = lock
            .0
            .lock()
            .map_err(|_| "连接配置正忙，请重试".to_owned())?;
        let root = app
            .path()
            .app_local_data_dir()
            .map_err(|_| "无法找到 Bridge 配置目录".to_owned())?;
        let mut repo = profiles::Repository::open(root).map_err(|error| error.to_string())?;
        task(&mut repo).map_err(|error| error.to_string())
    })
    .await
    .map_err(|_| "无法处理连接配置".to_owned())?
}

#[tauri::command]
async fn bridge_profiles(app: tauri::AppHandle) -> Result<profiles::ProfileList, String> {
    profile_task(app, |repo| Ok(repo.list())).await
}

#[tauri::command]
async fn bridge_profile_save(
    app: tauri::AppHandle,
    input: profiles::ProfileInput,
) -> Result<profiles::ProfileList, String> {
    profile_task(app, move |repo| repo.save(input)).await
}

#[tauri::command]
async fn bridge_profile_delete(
    app: tauri::AppHandle,
    state: AppState<'_, BridgeState>,
    id: String,
) -> Result<profiles::ProfileList, String> {
    let current = state.0.lock().await;
    if current
        .as_ref()
        .and_then(|bridge| bridge.profile_id.as_deref())
        == Some(&id)
    {
        return Err("此连接正在使用，请先停止服务或切换连接".into());
    }
    profile_task(app, move |repo| repo.delete(&id)).await
}

#[tauri::command]
async fn bridge_check(state: AppState<'_, BridgeState>) -> Result<(), String> {
    let connection = state
        .0
        .lock()
        .await
        .as_ref()
        .map(|value| value.connection.clone())
        .ok_or("本地服务未启动")?;
    tokio::select! {
        _ = connection.shutdown.cancelled() => Err("本地服务已停止".into()),
        result = async { let gateway = connection.gateway(); gateway.refresh(true).await?; gateway.verify_access().await.map(|_| ()) } => result,
    }
}

#[tauri::command]
async fn bridge_stop(state: AppState<'_, BridgeState>) -> Result<(), ()> {
    state.0.lock().await.take();
    Ok(())
}

#[tauri::command]
async fn bridge_launch(state: AppState<'_, BridgeState>, path: String) -> Result<(), String> {
    let current = state.0.lock().await;
    let bridge = current.as_ref().ok_or("本地服务未启动")?;
    let protection = bridge.protection.clone().ok_or("官方 API 代理未就绪")?;
    let connection = bridge.connection.clone();
    tauri::async_runtime::spawn_blocking(move || {
        let journal = protection.configure_cli().map_err(|e| e.to_string())?;
        if let Err(error) = launch::cli(
            &path,
            &protection.url(),
            &protection.secrets.local_token,
            &protection.ca_file(),
        ) {
            let _ = protection.restore_cli(&journal);
            return Err(error);
        }
        connection.cli_configured.store(true, Ordering::Relaxed);
        Ok(())
    })
    .await
    .map_err(|_| "无法启动 Claude Code")?
}

#[derive(Serialize)]
struct Clients {
    cli: bool,
    desktop: bool,
}

#[tauri::command]
async fn bridge_clients(path: String) -> Result<Clients, String> {
    tauri::async_runtime::spawn_blocking(move || Clients {
        cli: launch::has_cli(&path),
        desktop: launch::has_desktop(),
    })
    .await
    .map_err(|_| "无法检查客户端".into())
}

#[tauri::command]
async fn bridge_launch_desktop(
    app: tauri::AppHandle,
    state: AppState<'_, BridgeState>,
) -> Result<(), String> {
    let profile = desktop_settings(&app)?;
    // Serialize app configuration changes and prevent stop during a launch.
    let current = state.0.lock().await;
    let bridge = current.as_ref().ok_or("本地服务未启动")?;
    let key = bridge.connection.clone();
    let protection = bridge.protection.clone().ok_or("官方 API 代理未就绪")?;
    tauri::async_runtime::spawn_blocking(move || {
        protection
            .prepare_desktop_trust()
            .map_err(|e| e.to_string())?;
        key.desktop_trust.store(true, Ordering::Relaxed);
        profile
            .launch(&protection.url(), &protection.secrets.local_token)
            .map_err(|e| e.to_string())?;
        key.desktop_configured.store(true, Ordering::Relaxed);
        Ok(())
    })
    .await
    .map_err(|_| "无法启动 Claude Desktop")?
}

fn desktop_settings(app: &tauri::AppHandle) -> Result<desktop_profile::DesktopProfile, String> {
    let resolver = app.path();
    #[cfg(windows)]
    let data = resolver.local_data_dir();
    #[cfg(not(windows))]
    let data = resolver.data_dir();
    Ok(desktop_profile::DesktopProfile::new(
        data.map_err(|_| "无法找到 Desktop 配置目录")?,
        resolver
            .app_local_data_dir()
            .map_err(|_| "无法找到 Bridge 配置目录")?,
    ))
}

#[tauri::command]
fn bridge_desktop_configured(app: tauri::AppHandle) -> Result<bool, String> {
    Ok(desktop_settings(&app)?.has_undo())
}

#[tauri::command]
async fn bridge_restore_desktop(
    app: tauri::AppHandle,
    state: AppState<'_, BridgeState>,
) -> Result<bool, String> {
    let profile = desktop_settings(&app)?;
    let current = state.0.lock().await;
    let root = app
        .path()
        .app_local_data_dir()
        .map_err(|_| "无法找到 Bridge 配置目录")?
        .join("official-api");
    let result = tauri::async_runtime::spawn_blocking(move || {
        let kept = profile.restore().map_err(|e| e.to_string())?;
        if !kept {
            ikun_bridge_core::platform::remove_user_certificate(&root)
                .map_err(|e| format!("Desktop 配置已恢复，但证书撤销未完成：{e}"))?;
        }
        Ok(kept)
    })
    .await
    .map_err(|_| "无法恢复 Claude Desktop 配置")?;
    if let (Ok(kept), Some(bridge)) = (&result, current.as_ref()) {
        bridge
            .connection
            .desktop_configured
            .store(false, Ordering::Relaxed);
        if !kept {
            bridge
                .connection
                .desktop_trust
                .store(false, Ordering::Relaxed);
        }
    }
    result
}

#[tauri::command]
async fn bridge_restore_cli(
    app: tauri::AppHandle,
    state: AppState<'_, BridgeState>,
) -> Result<bool, String> {
    let current = state.0.lock().await;
    let root = app
        .path()
        .app_local_data_dir()
        .map_err(|_| "无法找到 Bridge 配置目录")?
        .join("official-api");
    let result = tauri::async_runtime::spawn_blocking(move || {
        let store = ikun_bridge_core::store::Store::new(root).map_err(|e| e.to_string())?;
        let mut kept = false;
        for journal in store.journals().map_err(|e| e.to_string())? {
            if journal.client == "cli" {
                kept |= !store
                    .undo(&journal.id)
                    .map_err(|e| e.to_string())?
                    .is_empty();
            }
        }
        Ok(kept)
    })
    .await
    .map_err(|_| "无法恢复 CLI 配置")?;
    if let (Ok(_), Some(bridge)) = (&result, current.as_ref()) {
        bridge
            .connection
            .cli_configured
            .store(false, Ordering::Relaxed);
    }
    result
}

#[tauri::command]
async fn bridge_quit(app: tauri::AppHandle, state: AppState<'_, BridgeState>) -> Result<(), ()> {
    app.state::<ExitIntent>().0.store(true, Ordering::Relaxed);
    state.0.lock().await.take();
    app.exit(0);
    Ok(())
}

fn routes(connection: Arc<Connection>) -> Router {
    Router::new()
        .route("/v1/messages", post(forward))
        .route("/v1/messages/count_tokens", post(forward))
        .route("/v1/models", get(forward))
        .route("/v1/models/{id}", get(forward))
        .layer(DefaultBodyLimit::max(MAX_BODY))
        .route_layer(middleware::from_fn_with_state(
            connection.clone(),
            authorize,
        ))
        .with_state(connection)
}

async fn authorize(
    State(connection): State<Arc<Connection>>,
    req: Request<Body>,
    next: Next,
) -> Response {
    let headers = req.headers();
    if headers
        .get(header::HOST)
        .and_then(|value| value.to_str().ok())
        != Some(&connection.host)
        || headers.contains_key(header::ORIGIN)
    {
        return error(StatusCode::FORBIDDEN, "仅允许本机客户端访问");
    }
    if connection.shutdown.is_cancelled() {
        return error(StatusCode::SERVICE_UNAVAILABLE, "本地服务已停止");
    }
    let key = headers
        .get("x-api-key")
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    let bearer = headers
        .get(header::AUTHORIZATION)
        .and_then(|v| v.to_str().ok())
        .and_then(|v| v.strip_prefix("Bearer "))
        .unwrap_or("");
    let cli = connection.client_key.as_bytes();
    let desktop = connection.desktop_key.as_bytes();
    if !bool::from(
        key.as_bytes().ct_eq(cli)
            | bearer.as_bytes().ct_eq(cli)
            | key.as_bytes().ct_eq(desktop)
            | bearer.as_bytes().ct_eq(desktop),
    ) {
        return error(StatusCode::UNAUTHORIZED, "本地接入凭据无效");
    }
    next.run(req).await
}

async fn forward(
    State(connection): State<Arc<Connection>>,
    method: Method,
    uri: Uri,
    headers: HeaderMap,
    body: Bytes,
) -> Response {
    connection.requests.fetch_add(1, Ordering::Relaxed);
    let gateway = connection.gateway();
    let response = tokio::select! {
        biased;
        _ = connection.shutdown.cancelled() => return error(StatusCode::SERVICE_UNAVAILABLE, "本地服务已停止"),
        response = gateway.forward(method, uri, headers, body) => response,
    };
    response
}

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _, _| {
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.show();
                let _ = window.unminimize();
                let _ = window.set_focus();
            }
        }))
        .setup(|app| {
            use tauri::{
                menu::{Menu, MenuItem},
                tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
            };
            let show = MenuItem::with_id(app, "show", "打开 IKUN Bridge", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&show])?;
            let mut tray = TrayIconBuilder::with_id("bridge")
                .tooltip("IKUN Bridge · 点击打开")
                .menu(&menu)
                .show_menu_on_left_click(false)
                .on_menu_event(|app, event| {
                    if event.id.as_ref() == "show" {
                        show_main(app);
                    }
                })
                .on_tray_icon_event(|tray, event| {
                    if matches!(
                        event,
                        TrayIconEvent::Click {
                            button: MouseButton::Left,
                            button_state: MouseButtonState::Up,
                            ..
                        }
                    ) {
                        show_main(tray.app_handle());
                    }
                });
            if let Some(icon) = app.default_window_icon() {
                tray = tray.icon(icon.clone());
            }
            tray.build(app)?;
            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = window.hide();
            }
        })
        .manage(BridgeState::default())
        .manage(ExitIntent::default())
        .manage(ProfilesLock::default())
        .invoke_handler(tauri::generate_handler![
            bridge_status,
            bridge_start,
            bridge_profiles,
            bridge_profile_save,
            bridge_profile_delete,
            bridge_check,
            bridge_stop,
            bridge_clients,
            bridge_launch,
            bridge_launch_desktop,
            bridge_desktop_configured,
            bridge_restore_desktop,
            bridge_restore_cli,
            bridge_quit
        ])
        .build(tauri::generate_context!())
        .expect("启动 IKUN Bridge 失败")
        .run(|app, event| {
            if let tauri::RunEvent::ExitRequested { api, .. } = event {
                if !app.state::<ExitIntent>().0.load(Ordering::Relaxed) {
                    api.prevent_exit();
                }
            }
        });
}

fn show_main(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
}
