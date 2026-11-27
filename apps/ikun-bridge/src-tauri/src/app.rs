use crate::{launcher, platform, proxy, settings::Settings, store::Store};
use serde::Serialize;
use std::{path::PathBuf, sync::Arc};
use tauri::{Manager, State};
use tokio::sync::Mutex;
struct Session {
    settings: Settings,
    running: Option<proxy::RunningProxy>,
}
pub struct AppState {
    store: Store,
    session: Mutex<Session>,
}
#[derive(Serialize)]
pub struct UndoRow {
    id: String,
    client: String,
    files: Vec<String>,
    pending: bool,
}
#[derive(Serialize)]
pub struct View {
    settings: Settings,
    key_saved: bool,
    running: bool,
    stats: proxy::Stats,
    undo: Vec<UndoRow>,
    version: &'static str,
    platform: &'static str,
}
fn msg(e: anyhow::Error) -> String {
    format!("{e:#}")
}
#[tauri::command]
async fn view(state: State<'_, Arc<AppState>>) -> Result<View, String> {
    let s = state.session.lock().await;
    let key_saved = state
        .store
        .existing_secrets()
        .map_err(msg)?
        .is_some_and(|s| !s.gateway_key.is_empty());
    let undo = state
        .store
        .journals()
        .map_err(msg)?
        .into_iter()
        .map(|j| UndoRow {
            id: j.id,
            client: j.client,
            files: j
                .files
                .iter()
                .map(|f| f.path.display().to_string())
                .collect(),
            pending: j.pending,
        })
        .collect();
    Ok(View {
        settings: s.settings.clone(),
        key_saved,
        running: s.running.as_ref().is_some_and(|r| r.alive()),
        stats: s
            .running
            .as_ref()
            .map(|r| r.metrics.snapshot())
            .unwrap_or(proxy::Stats {
                forwarded: 0,
                blocked: 0,
                filtered: 0,
                events: vec![],
            }),
        undo,
        version: env!("CARGO_PKG_VERSION"),
        platform: std::env::consts::OS,
    })
}
#[tauri::command]
async fn save_settings(
    state: State<'_, Arc<AppState>>,
    settings: Settings,
    key: Option<String>,
) -> Result<(), String> {
    let mut s = state.session.lock().await;
    if s.running.is_some() {
        return Err("请先停止接管，再修改连接设置".into());
    }
    settings.save(&state.store.root).map_err(msg)?;
    if let Some(key) = key {
        if !key.trim().is_empty() {
            let mut secrets = state.store.secrets().map_err(msg)?;
            secrets.gateway_key = key.trim().into();
            state.store.save_secrets(&secrets).map_err(msg)?;
        }
    }
    s.settings = settings;
    Ok(())
}
#[tauri::command]
async fn check(state: State<'_, Arc<AppState>>) -> Result<launcher::Inspection, String> {
    let s = state.session.lock().await;
    launcher::inspect(&s.settings, &state.store).map_err(msg)
}
async fn ensure_proxy(state: &AppState, s: &mut Session) -> Result<(), String> {
    if s.running.as_ref().is_some_and(|r| r.alive()) {
        return Ok(());
    }
    if let Some(r) = s.running.take() {
        r.stop().await;
    }
    let mut secrets = state.store.secrets().map_err(msg)?;
    proxy::ensure_certificates(&state.store, &mut secrets).map_err(msg)?;
    let r = proxy::start(&s.settings, &secrets, false)
        .await
        .map_err(msg)?;
    if !proxy::health(r.port, &r.nonce).await {
        r.stop().await;
        return Err("本地服务就绪检查失败".into());
    }
    s.running = Some(r);
    Ok(())
}
#[tauri::command]
async fn start_bridge(state: State<'_, Arc<AppState>>) -> Result<(), String> {
    let mut s = state.session.lock().await;
    ensure_proxy(&state, &mut s).await
}
#[tauri::command]
async fn stop_bridge(state: State<'_, Arc<AppState>>) -> Result<(), String> {
    let mut s = state.session.lock().await;
    platform::ensure_idle(&state.store.root).map_err(msg)?;
    if let Some(r) = s.running.take() {
        r.stop().await;
    }
    Ok(())
}
#[tauri::command]
async fn launch(state: State<'_, Arc<AppState>>, client: String) -> Result<u32, String> {
    if client != "cli" || !cfg!(windows) {
        return Err("此客户端的出口限制尚未完成实机验证，启动未启用".into());
    }
    let mut s = state.session.lock().await;
    launcher::cli_executable(&s.settings).map_err(msg)?;
    crate::settings::check_cli_profile(&s.settings).map_err(msg)?;
    ensure_proxy(&state, &mut s).await?;
    let secrets = state.store.secrets().map_err(msg)?;
    launcher::launch_cli(&s.settings, &state.store, &secrets).map_err(msg)
}
#[tauri::command]
async fn restore(
    state: State<'_, Arc<AppState>>,
    id: Option<String>,
) -> Result<Vec<String>, String> {
    let mut s = state.session.lock().await;
    platform::ensure_idle(&state.store.root).map_err(msg)?;
    if let Some(r) = s.running.take() {
        r.stop().await;
    }
    if let Some(id) = id {
        return state.store.undo(&id).map_err(msg);
    }
    let mut conflicts = vec![];
    for j in state.store.journals().map_err(msg)? {
        conflicts.extend(state.store.undo(&j.id).map_err(msg)?);
    }
    if conflicts.is_empty() {
        platform::remove_guards_elevated(&state.store.root).map_err(msg)?;
        platform::remove_user_certificate(&state.store.root).map_err(msg)?;
    }
    Ok(conflicts)
}
pub fn run() {
    let root = if cfg!(debug_assertions) {
        std::env::var_os("IKUN_BRIDGE_DATA_DIR")
            .map(PathBuf::from)
            .map(Ok)
            .unwrap_or_else(platform::default_root)
    } else {
        platform::default_root()
    };
    let state = match root.and_then(Store::new).and_then(|store| {
        let settings = Settings::load(&store.root)?;
        Ok(Arc::new(AppState {
            store,
            session: Mutex::new(Session {
                settings,
                running: None,
            }),
        }))
    }) {
        Ok(s) => s,
        Err(e) => {
            eprintln!("IKUN Bridge: {e:#}");
            return;
        }
    };
    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _, _| {
            if let Some(w) = app.get_webview_window("main") {
                let _ = w.show();
                let _ = w.set_focus();
            }
        }))
        .plugin(tauri_plugin_dialog::init())
        .manage(state)
        .invoke_handler(tauri::generate_handler![
            view,
            save_settings,
            check,
            start_bridge,
            stop_bridge,
            launch,
            restore
        ])
        .setup(|app| {
            use tauri::{
                menu::{Menu, MenuItem},
                tray::TrayIconBuilder,
            };
            let show = MenuItem::with_id(app, "show", "打开 IKUN Bridge", true, None::<&str>)?;
            let quit = MenuItem::with_id(app, "quit", "退出", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&show, &quit])?;
            let icon = tauri::image::Image::new_owned([32, 92, 222, 255].repeat(32 * 32), 32, 32);
            let icon = app.default_window_icon().cloned().unwrap_or(icon);
            TrayIconBuilder::new()
                .icon(icon)
                .tooltip("IKUN Bridge")
                .menu(&menu)
                .on_menu_event(|app, event| match event.id.as_ref() {
                    "show" => {
                        if let Some(w) = app.get_webview_window("main") {
                            let _ = w.show();
                            let _ = w.set_focus();
                        }
                    }
                    "quit" => app.exit(0),
                    _ => {}
                })
                .build(app)?;
            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = window.hide();
            }
        })
        .run(tauri::generate_context!())
        .expect("启动 IKUN Bridge 失败");
}
