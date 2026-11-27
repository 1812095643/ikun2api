//! Explicit opt-in: only the supplied original binary is copied into a test profile.
use anyhow::{Context, Result};
use bytes::Bytes;
use http_body_util::{BodyExt, Full};
use hyper::{body::Incoming, service::service_fn, Request, Response};
use hyper_util::rt::TokioIo;
use ikun_bridge_lib::{
    launcher, platform, proxy,
    settings::{self, Settings},
    store::Store,
};
use std::{
    convert::Infallible,
    path::PathBuf,
    sync::{
        atomic::{AtomicUsize, Ordering},
        Arc, Mutex,
    },
};
use tokio::net::TcpListener;

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
#[ignore = "Requires IKUN_TEST_CLAUDE_EXE and administrator rights for isolated WFP coverage"]
async fn original_cli_through_rust_bridge_with_tool_and_real_guard() -> Result<()> {
    let original = PathBuf::from(
        std::env::var_os("IKUN_TEST_CLAUDE_EXE")
            .context("Set IKUN_TEST_CLAUDE_EXE to the verified 2.1.326 executable")?,
    );
    let temp = tempfile::tempdir()?;
    let copied = temp.path().join("claude-local-fixture.exe");
    std::fs::copy(original, &copied)?;
    let private = temp.path().join("bridge");
    let store = Store::new(private.clone())?;
    let mut secrets = store.secrets()?;
    secrets.gateway_key = "SYNTHETIC_GATEWAY_KEY_NO_PAID_CALLS".into();
    proxy::ensure_certificates(&store, &mut secrets)?;
    let user = temp.path().join("profile");
    let config = user.join(".claude");
    let repo = user.join("repo");
    std::fs::create_dir_all(&config)?;
    std::fs::create_dir_all(&repo)?;
    for name in ["tmp", "appdata", "localappdata"] {
        std::fs::create_dir_all(user.join(name))?;
    }
    let file = repo.join("CANARY_中文.txt");
    std::fs::write(&file, "NATIVE_TOOL_RESULT_中文_😀\n")?;
    std::fs::write(
        config.join(".claude.json"),
        serde_json::to_vec(
            &serde_json::json!({"hasCompletedOnboarding":true,"autoUpdates":false,"projects":{repo.display().to_string():{"hasTrustDialogAccepted":true}}}),
        )?,
    )?;
    let upstream = TcpListener::bind("127.0.0.1:0").await?;
    let address = upstream.local_addr()?;
    let count = Arc::new(AtomicUsize::new(0));
    let records = Arc::new(Mutex::new(vec![]));
    let backend = tokio::spawn({
        let count = count.clone();
        let records = records.clone();
        async move {
            loop {
                let Ok((stream, _)) = upstream.accept().await else {
                    break;
                };
                let count = count.clone();
                let records = records.clone();
                let file = file.clone();
                tokio::spawn(async move {
                    let _=hyper::server::conn::http1::Builder::new().serve_connection(TokioIo::new(stream),service_fn(move|req:Request<Incoming>|{let records=records.clone();let count=count.clone();let file=file.clone();async move{
        assert_eq!(req.headers()["x-api-key"],"SYNTHETIC_GATEWAY_KEY_NO_PAID_CALLS");let raw=req.into_body().collect().await.unwrap().to_bytes();let body:serde_json::Value=serde_json::from_slice(&raw).unwrap();let n=count.fetch_add(1,Ordering::SeqCst);let model=body["model"].as_str().unwrap().to_string();records.lock().unwrap().push(body);
        let mut events=vec![("message_start",serde_json::json!({"type":"message_start","message":{"id":format!("msg_LOCAL_{n}"),"type":"message","role":"assistant","model":model,"content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":0}}}))];
        if n==0{events.extend([("content_block_start",serde_json::json!({"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_LOCAL_BRIDGE","name":"Read","input":{}}})),("content_block_delta",serde_json::json!({"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":serde_json::to_string(&serde_json::json!({"file_path":file})).unwrap()}}))]);}else{events.extend([("content_block_start",serde_json::json!({"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}})),("content_block_delta",serde_json::json!({"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"NATIVE_RUST_BRIDGE_OK_中文_😀"}}))]);}
        events.extend([("content_block_stop",serde_json::json!({"type":"content_block_stop","index":0})),("message_delta",serde_json::json!({"type":"message_delta","delta":{"stop_reason":if n==0{"tool_use"}else{"end_turn"},"stop_sequence":null},"usage":{"output_tokens":8}})),("message_stop",serde_json::json!({"type":"message_stop"}))]);
        let sse=events.into_iter().map(|(e,v)|format!("event: {e}\ndata: {v}\n\n")).collect::<String>();Ok::<_,Infallible>(Response::builder().header("content-type","text/event-stream").body(Full::new(Bytes::from(sse))).unwrap())
    }})).await;
                });
            }
        }
    });
    let mut settings = Settings {
        gateway_url: format!("http://{address}"),
        listen_port: 0,
        ..Default::default()
    };
    let running = proxy::start(&settings, &secrets, true).await?;
    settings.listen_port = running.port;
    settings.cli_config = config.join("settings.json").display().to_string();
    let journal = store.apply("cli", settings::prepare_cli(&settings, &secrets)?)?;
    #[cfg(windows)]
    platform::install_guard(&store.root, &copied, running.port)?;
    struct Cleanup(PathBuf);
    impl Drop for Cleanup {
        fn drop(&mut self) {
            let _ = platform::remove_guards(&self.0);
        }
    }
    let _cleanup = Cleanup(store.root.clone());
    let mut command = std::process::Command::new(&copied);
    command.env_clear();
    for k in ["SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "SYSTEMDRIVE"] {
        if let Some(v) = std::env::var_os(k) {
            command.env(k, v);
        }
    }
    command
        .env(
            "PATH",
            r"C:\Windows\system32;C:\Windows;C:\Program Files\Git\cmd;C:\Program Files\Git\bin",
        )
        .env("HOME", &user)
        .env("USERPROFILE", &user)
        .env("CLAUDE_CONFIG_DIR", &config)
        .env("APPDATA", user.join("appdata"))
        .env("LOCALAPPDATA", user.join("localappdata"))
        .env("TEMP", user.join("tmp"))
        .env("TMP", user.join("tmp"))
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .env("GIT_CONFIG_GLOBAL", user.join("empty.gitconfig"))
        .env(
            "CLAUDE_CODE_GIT_BASH_PATH",
            r"C:\Program Files\Git\bin\bash.exe",
        )
        .env("API_TIMEOUT_MS", "8000");
    launcher::configure_env(&mut command, &settings.proxy_url(), &store, &secrets);
    command.current_dir(&repo).args([
        "--print",
        "Read the local fixture when asked, then reply.",
        "--model",
        "claude-sonnet-4-6",
        "--max-turns",
        "3",
        "--no-session-persistence",
        "--strict-mcp-config",
        "--mcp-config",
        r#"{"mcpServers":{}}"#,
        "--setting-sources",
        "user",
        "--settings",
        r#"{"disableAllHooks":true,"autoMemoryEnabled":false}"#,
        "--permission-mode",
        "dontAsk",
        "--no-chrome",
        "--tools",
        "Read",
        "--allowedTools",
        "Read",
    ]);
    let child = platform::hidden(&mut command)
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .spawn()?;
    let result = tokio::task::spawn_blocking(move || child.wait_with_output()).await??;
    assert!(
        result.status.success(),
        "CLI status {:?}, stdout {}, stderr {}",
        result.status,
        String::from_utf8_lossy(&result.stdout),
        String::from_utf8_lossy(&result.stderr)
    );
    assert!(String::from_utf8_lossy(&result.stdout).contains("NATIVE_RUST_BRIDGE_OK_中文_😀"));
    {
        let rows = records.lock().unwrap();
        assert_eq!(rows.len(), 2);
        let text = serde_json::to_string(&rows[1])?;
        assert!(text.contains("NATIVE_TOOL_RESULT_中文_😀"));
        assert!(!text.contains("x-anthropic-billing-header"));
        assert!(rows[0].get("metadata").is_none());
    }
    assert!(store.undo(&journal)?.is_empty());
    assert!(!config.join("settings.json").exists());
    running.stop().await;
    backend.abort();
    platform::remove_guards(&store.root)?;
    println!("Official CLI: official hostname -> Rust TLS proxy -> filtered local gateway -> Read tool -> final SSE; WFP enabled; exact config undo passed; no paid calls");
    Ok(())
}
