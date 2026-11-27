use super::*;
use axum::{http::HeaderMap, Json};
use serde_json::json;
use std::sync::atomic::AtomicUsize;

#[tokio::test]
async fn switching_gateway_keeps_local_listener_and_inflight_request() {
    let started = Arc::new(tokio::sync::Notify::new());
    let release = Arc::new(tokio::sync::Notify::new());
    let old = Router::new()
        .route(
            "/v1/ikun/bridge/session",
            post(|| async { Json(json!({"ticket":"A".repeat(66),"expires_in":300})) }),
        )
        .route(
            "/v1/messages",
            post({
                let started = started.clone();
                let release = release.clone();
                move |headers: HeaderMap| {
                    let started = started.clone();
                    let release = release.clone();
                    async move {
                        assert_eq!(headers["x-api-key"], "old-private-key");
                        started.notify_one();
                        release.notified().await;
                        "old"
                    }
                }
            }),
        );
    let new = Router::new()
        .route(
            "/v1/ikun/bridge/session",
            post(|| async { Json(json!({"ticket":"B".repeat(66),"expires_in":300})) }),
        )
        .route(
            "/v1/messages",
            post(|headers: HeaderMap| async move {
                assert_eq!(headers["x-api-key"], "new-private-key");
                assert_eq!(headers["x-ikun-bridge-pass"], "B".repeat(66));
                "new"
            }),
        );
    let old_listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let new_listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let old_url = format!("http://{}/", old_listener.local_addr().unwrap())
        .parse()
        .unwrap();
    let new_url = format!("http://{}/", new_listener.local_addr().unwrap())
        .parse()
        .unwrap();
    let old_server = tokio::spawn(async move { axum::serve(old_listener, old).await.unwrap() });
    let new_server = tokio::spawn(async move { axum::serve(new_listener, new).await.unwrap() });
    let bridge = LocalBridge::serve(
        TcpListener::bind("127.0.0.1:0").await.unwrap(),
        Gateway::local_fixture(old_url, "old-private-key".into()),
        Zeroizing::new("local-only".into()),
    )
    .unwrap();
    let endpoint = format!("{}/v1/messages", bridge.endpoint);
    let client = reqwest::Client::builder().no_proxy().build().unwrap();
    let first = tokio::spawn(
        client
            .post(&endpoint)
            .header("x-api-key", "local-only")
            .send(),
    );
    tokio::time::timeout(Duration::from_secs(5), started.notified())
        .await
        .unwrap();
    bridge
        .connection
        .switch_gateway(Gateway::local_fixture(new_url, "new-private-key".into()));
    let second = client
        .post(&endpoint)
        .header("x-api-key", "local-only")
        .send()
        .await
        .unwrap();
    assert_eq!(second.text().await.unwrap(), "new");
    release.notify_one();
    assert_eq!(first.await.unwrap().unwrap().text().await.unwrap(), "old");
    assert_eq!(bridge.connection.requests.load(Ordering::Relaxed), 2);
    old_server.abort();
    new_server.abort();
}

#[tokio::test]
#[ignore = "需要与 Go 的 TestBridgePassUsesAuthenticatedAPIKey 联调，只访问本机接收器"]
async fn go_gateway_authenticates_the_actual_bridge_pass() {
    let origin = std::env::var("IKUN_BRIDGE_INTEROP_ORIGIN").expect("需要本机 Go 接收器地址");
    let origin = reqwest::Url::parse(&format!("{}/", origin.trim_end_matches('/'))).unwrap();
    assert_eq!(origin.scheme(), "http");
    assert_eq!(origin.host_str(), Some("127.0.0.1"));
    let client = reqwest::Client::builder().no_proxy().build().unwrap();
    let direct = client
        .get(origin.join("v1/models").unwrap())
        .header("x-api-key", "bridge-real-auth-key")
        .send()
        .await
        .unwrap();
    assert_eq!(direct.status(), StatusCode::FORBIDDEN);
    assert_eq!(
        direct.json::<serde_json::Value>().await.unwrap()["error"]["code"],
        "bridge_required"
    );
    let gateway = Gateway::local_fixture(origin.clone(), "bridge-real-auth-key".into());
    gateway.refresh(true).await.unwrap();
    assert!(gateway.verify_access().await.unwrap());
    let mut bridge = LocalBridge::serve(
        TcpListener::bind("127.0.0.1:0").await.unwrap(),
        gateway,
        Zeroizing::new("fixture-relay-key".into()),
    )
    .unwrap();
    let directory = tempfile::tempdir().unwrap();
    let protection = Arc::new(
        protection::Protection::start(directory.path(), &bridge.endpoint, "fixture-relay-key")
            .await
            .unwrap(),
    );
    bridge.protection = Some(protection.clone());
    let desktop = reqwest::Client::builder()
        .no_proxy()
        .proxy(reqwest::Proxy::all(protection.url()).unwrap())
        .add_root_certificate(
            reqwest::Certificate::from_pem(protection.secrets.ca_cert.as_bytes()).unwrap(),
        )
        .build()
        .unwrap();
    let response = desktop
        .post("https://api.anthropic.com/v1/messages")
        .header("x-api-key", protection.secrets.local_token.as_str())
        .header("user-agent", "claude-cli/2.1.326 (external, claude-desktop-3p)")
        .header("x-app", "cli")
        .header("anthropic-beta", "claude-code-20250219")
        .header("anthropic-version", "2023-06-01")
        .json(&json!({"model":"claude-test","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"system":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."}],"metadata":{"user_id":"private-canary"}}))
        .send().await.unwrap();
    assert_eq!(response.status(), StatusCode::OK);
    assert!(bridge.connection.gateway().admission().await.0);
    let done = client
        .post(origin.join("_fixture/done").unwrap())
        .send()
        .await
        .unwrap();
    assert_eq!(done.status(), StatusCode::NO_CONTENT);
}

// 使用真实 CONNECT/TLS、过滤器和本地转发链路，不安装系统证书、不调用付费模型。
#[tokio::test]
async fn official_api_route_filters_and_attaches_bridge_pass() {
    let calls = Arc::new(AtomicUsize::new(0));
    let called = calls.clone();
    let router = Router::new()
        .route(
            "/v1/ikun/bridge/session",
            post(|headers: HeaderMap| async move {
                assert_eq!(headers["x-api-key"], "fixture-upstream-key");
                Json(json!({"ticket": "A".repeat(66), "expires_in": 300}))
            }),
        )
        .route(
            "/v1/models",
            get(|headers: HeaderMap| async move {
                assert_eq!(headers["x-ikun-bridge-pass"], "A".repeat(66));
                Json(json!({"data": []}))
            }),
        )
        .route(
            "/v1/messages",
            post(move |headers: HeaderMap, body: Bytes| {
                let calls = called.clone();
                async move {
                    calls.fetch_add(1, Ordering::Relaxed);
                    assert_eq!(headers["x-api-key"], "fixture-upstream-key");
                    assert_eq!(headers["x-ikun-bridge-pass"], "A".repeat(66));
                    assert_eq!(
                        headers["user-agent"],
                        "claude-cli/2.1.326 (external, claude-desktop-3p)"
                    );
                    assert_eq!(headers["x-app"], "cli");
                    assert!(!headers.contains_key("authorization"));
                    let value: serde_json::Value = serde_json::from_slice(&body).unwrap();
                    assert!(value.get("metadata").is_none());
                    assert!(!String::from_utf8_lossy(&body).contains("private-canary"));
                    assert_eq!(value["messages"][0]["content"], "hello 中文 😀");
                    (
                        [("content-type", "text/event-stream")],
                        "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
                    )
                }
            }),
        );
    let remote = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let base = format!("http://{}/", remote.local_addr().unwrap())
        .parse()
        .unwrap();
    let server = tokio::spawn(async move { axum::serve(remote, router).await.unwrap() });
    let gateway = Gateway::local_fixture(base, "fixture-upstream-key".into());
    gateway.refresh(true).await.unwrap();
    assert!(gateway.verify_access().await.unwrap());
    assert_eq!(calls.load(Ordering::Relaxed), 0, "准入检查不能调用模型");
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let mut bridge = LocalBridge::serve(
        listener,
        gateway,
        Zeroizing::new("fixture-local-key".into()),
    )
    .unwrap();
    let directory = tempfile::tempdir().unwrap();
    let protected = Arc::new(
        protection::Protection::start(directory.path(), &bridge.endpoint, "fixture-local-key")
            .await
            .unwrap(),
    );
    bridge.protection = Some(protected.clone());
    let client = reqwest::Client::builder()
        .no_proxy()
        .proxy(reqwest::Proxy::all(protected.url()).unwrap())
        .add_root_certificate(
            reqwest::Certificate::from_pem(protected.secrets.ca_cert.as_bytes()).unwrap(),
        )
        .build()
        .unwrap();
    let response = client.post("https://api.anthropic.com/v1/messages")
        .header("x-api-key", protected.secrets.local_token.as_str())
        .header("user-agent", "claude-cli/2.1.326 (external, claude-desktop-3p)")
        .header("x-app", "cli")
        .json(&json!({"model":"claude-test", "messages":[{"role":"user","content":"hello 中文 😀"}], "metadata":{"leak":"private-canary"}, "max_tokens":16, "stream":true}))
        .send().await.unwrap();
    assert_eq!(response.status(), StatusCode::OK);
    assert!(response.text().await.unwrap().contains("message_stop"));
    assert_eq!(calls.load(Ordering::Relaxed), 1);
    assert_eq!(bridge.connection.requests.load(Ordering::Relaxed), 1);
    assert_eq!(protected.proxy.metrics.snapshot().filtered, 1);
    let refused = client
        .post("https://api.anthropic.com/api/event_logging/batch")
        .header("x-api-key", protected.secrets.local_token.as_str())
        .body("{}")
        .send()
        .await
        .unwrap();
    assert_eq!(refused.status(), StatusCode::FORBIDDEN);
    assert_eq!(calls.load(Ordering::Relaxed), 1);
    assert!(protected.proxy.metrics.snapshot().blocked > 0);
    drop(bridge);
    assert!(protected.proxy.cancel.is_cancelled());
    server.abort();
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "需要 IKUN_TEST_CLAUDE_EXE 指向已核验的官方 CLI，只访问本地协议接收器"]
async fn original_cli_uses_official_hostname_and_local_bridge() {
    let executable = std::env::var_os("IKUN_TEST_CLAUDE_EXE").expect("需要官方 CLI 路径");
    let calls = Arc::new(AtomicUsize::new(0));
    let called = calls.clone();
    let router = Router::new()
        .route("/v1/ikun/bridge/session", post(|| async { Json(json!({"ticket":"A".repeat(66), "expires_in":300})) }))
        .route("/v1/messages", post(move |headers: HeaderMap, body: Bytes| {
            let calls = called.clone();
            async move {
                assert_eq!(headers["x-api-key"], "fixture-upstream-key");
                assert_eq!(headers["x-ikun-bridge-pass"], "A".repeat(66));
                let value: serde_json::Value = serde_json::from_slice(&body).unwrap();
                assert!(value.get("metadata").is_none());
                calls.fetch_add(1, Ordering::Relaxed);
                let events = [
                    ("message_start",json!({"type":"message_start","message":{"id":"msg_bridge_local","type":"message","role":"assistant","model":value["model"],"content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":0}}})),
                    ("content_block_start",json!({"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}})),
                    ("content_block_delta",json!({"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"BRIDGE_OFFICIAL_ROUTE_OK_中文"}})),
                    ("content_block_stop",json!({"type":"content_block_stop","index":0})),
                    ("message_delta",json!({"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":8}})),
                    ("message_stop",json!({"type":"message_stop"}))
                ];
                let sse = events.into_iter().map(|(name,value)|format!("event: {name}\ndata: {value}\n\n")).collect::<String>();
                ([("content-type","text/event-stream")],sse)
            }
        }));
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let base = format!("http://{}/", listener.local_addr().unwrap())
        .parse()
        .unwrap();
    let server = tokio::spawn(async move { axum::serve(listener, router).await.unwrap() });
    let gateway = Gateway::local_fixture(base, "fixture-upstream-key".into());
    let bridge = LocalBridge::serve(
        TcpListener::bind("127.0.0.1:0").await.unwrap(),
        gateway,
        Zeroizing::new("fixture-relay-key".into()),
    )
    .unwrap();
    let directory = tempfile::tempdir().unwrap();
    let protected =
        protection::Protection::start(directory.path(), &bridge.endpoint, "fixture-relay-key")
            .await
            .unwrap();
    let config = directory.path().join("cli");
    let work = directory.path().join("work");
    std::fs::create_dir_all(&config).unwrap();
    std::fs::create_dir_all(&work).unwrap();
    std::fs::write(
        config.join(".claude.json"),
        r#"{"hasCompletedOnboarding":true,"theme":"dark"}"#,
    )
    .unwrap();
    let mut command = std::process::Command::new(executable);
    command.env_clear();
    for name in [
        "SYSTEMROOT",
        "WINDIR",
        "COMSPEC",
        "PATH",
        "PATHEXT",
        "SYSTEMDRIVE",
        "TEMP",
        "TMP",
    ] {
        if let Some(value) = std::env::var_os(name) {
            command.env(name, value);
        }
    }
    let settings = ikun_bridge_core::settings::Settings {
        listen_port: protected.proxy.port,
        ..Default::default()
    };
    ikun_bridge_core::launcher::configure_env(
        &mut command,
        &protected.url(),
        &protected.store,
        &protected.secrets,
    );
    command
        .env_remove("ANTHROPIC_AUTH_TOKEN")
        .env("CLAUDE_CONFIG_DIR", &config)
        .env("HOME", directory.path())
        .env("USERPROFILE", directory.path())
        .env("APPDATA", directory.path().join("appdata"))
        .env("LOCALAPPDATA", directory.path().join("localappdata"))
        .env("API_TIMEOUT_MS", "10000")
        .current_dir(&work)
        .args([
            "--print",
            "Reply with a short greeting.",
            "--model",
            "claude-sonnet-4-6",
            "--max-turns",
            "1",
            "--no-session-persistence",
            "--strict-mcp-config",
            "--mcp-config",
            r#"{"mcpServers":{}}"#,
            "--setting-sources",
            "user",
            "--settings",
            r#"{"disableAllHooks":true,"autoMemoryEnabled":false}"#,
            "--no-chrome",
            "--tools",
            "",
        ]);
    let mut edits = ikun_bridge_core::settings::cli_changes(&settings, &protected.secrets);
    edits.retain(|edit| {
        edit.target
            != ikun_bridge_core::config_edit::Target::field(&["env", "ANTHROPIC_AUTH_TOKEN"])
    });
    let doc = ikun_bridge_core::config_edit::FileRecord::prepare(
        &config.join("settings.json"),
        &edits,
        true,
    )
    .unwrap();
    doc.write().unwrap();
    let output = tokio::task::spawn_blocking(move || {
        ikun_bridge_core::platform::hidden(&mut command)
            .output()
            .unwrap()
    })
    .await
    .unwrap();
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    assert!(String::from_utf8_lossy(&output.stdout).contains("BRIDGE_OFFICIAL_ROUTE_OK_中文"));
    assert!(calls.load(Ordering::Relaxed) > 0);
    server.abort();
}
