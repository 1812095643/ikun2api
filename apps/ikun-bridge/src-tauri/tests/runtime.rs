use anyhow::Result;
use bytes::Bytes;
use http_body_util::{BodyExt, Full};
use hyper::{body::Incoming, service::service_fn, Request, Response};
use hyper_util::rt::TokioIo;
use ikun_bridge_lib::{proxy, settings::Settings, store::Store};
use std::{
    convert::Infallible,
    sync::{Arc, Mutex},
};
use tokio::net::TcpListener;

#[tokio::test]
async fn real_tls_proxy_filter_auth_redirect_and_shutdown() -> Result<()> {
    let dir = tempfile::tempdir()?;
    let store = Store::new(dir.path().join("private"))?;
    let mut secrets = store.secrets()?;
    secrets.gateway_key = "SYNTHETIC_UPSTREAM_ONLY".into();
    proxy::ensure_certificates(&store, &mut secrets)?;
    let received = Arc::new(Mutex::new(vec![]));
    let upstream = TcpListener::bind("127.0.0.1:0").await?;
    let addr = upstream.local_addr()?;
    let backend = tokio::spawn({
        let received = received.clone();
        async move {
            loop {
                let Ok((stream, _)) = upstream.accept().await else {
                    break;
                };
                let received = received.clone();
                tokio::spawn(async move {
                    let _=hyper::server::conn::http1::Builder::new().serve_connection(TokioIo::new(stream),service_fn(move|r:Request<Incoming>|{let received=received.clone();async move{
        let (parts,body)=r.into_parts();let b=body.collect().await.unwrap().to_bytes();received.lock().unwrap().push((parts.headers,serde_json::from_slice::<serde_json::Value>(&b).unwrap_or_default()));
        let response=if parts.uri.query()==Some("beta=true"){Response::builder().status(307).header("location","http://127.0.0.1:1/never-send-key").body(Full::new(Bytes::new())).unwrap()}else{Response::builder().header("content-type","text/event-stream").body(Full::new(Bytes::from_static(b"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))).unwrap()};Ok::<_,Infallible>(response)
    }})).await;
                });
            }
        }
    });
    let settings = Settings {
        gateway_url: format!("http://{addr}"),
        listen_port: 0,
        ..Default::default()
    };
    let running = proxy::start(&settings, &secrets, true).await?;
    assert!(proxy::health(running.port, &running.nonce).await);
    let p = format!("http://127.0.0.1:{}", running.port);
    let client = reqwest::Client::builder()
        .no_proxy()
        .proxy(reqwest::Proxy::https(&p)?)
        .add_root_certificate(reqwest::Certificate::from_pem(secrets.ca_cert.as_bytes())?)
        .redirect(reqwest::redirect::Policy::none())
        .build()?;
    let body = serde_json::json!({"model":"claude-test","messages":[{"role":"user","content":"hello 中文 😀"}],"system":"Keep this\nx-anthropic-billing-header: cc_gateway=1;\n","metadata":{"leak":"canary"},"tools":[{"name":"Read","input_schema":{"type":"object"},"ownership_metadata":{"server_url":"https://private.invalid"}}]});
    let response = client
        .post("https://api.anthropic.com/v1/messages")
        .header("x-api-key", &secrets.local_token)
        .header("x-claude-code-wiggly-dove", "private")
        .json(&body)
        .send()
        .await?;
    assert_eq!(response.status(), 200);
    assert!(response.text().await?.contains("message_stop"));
    {
        let rows = received.lock().unwrap();
        assert_eq!(rows.len(), 1);
        assert_eq!(rows[0].0["x-api-key"], "SYNTHETIC_UPSTREAM_ONLY");
        assert!(!rows[0].0.contains_key("x-claude-code-wiggly-dove"));
        assert!(rows[0].1.get("metadata").is_none());
        assert_eq!(rows[0].1["messages"][0]["content"], "hello 中文 😀");
        assert_eq!(rows[0].1["system"], "Keep this\n");
    }
    assert_eq!(
        client
            .post("https://api.anthropic.com/v1/messages")
            .header("x-api-key", "WRONG")
            .json(&body)
            .send()
            .await?
            .status(),
        401
    );
    assert_eq!(
        client
            .post("https://api.anthropic.com/api/event_logging/v2/batch")
            .header("x-api-key", &secrets.local_token)
            .json(&body)
            .send()
            .await?
            .status(),
        403
    );
    assert_eq!(
        client
            .post("https://api.anthropic.com/v1/messages?beta=true")
            .header("x-api-key", &secrets.local_token)
            .json(&body)
            .send()
            .await?
            .status(),
        502
    );
    assert_eq!(received.lock().unwrap().len(), 2);
    let port = running.port;
    running.stop().await;
    assert!(tokio::net::TcpStream::connect(("127.0.0.1", port))
        .await
        .is_err());
    backend.abort();
    Ok(())
}

#[cfg(windows)]
#[test]
#[ignore = "Installs temporary WFP rules for an isolated copied test executable; administrator required"]
fn windows_guard_blocks_wrong_port_and_ipv6_then_restores() -> Result<()> {
    use ikun_bridge_lib::platform;
    let dir = tempfile::tempdir()?;
    let program = dir.path().join("bridge-guard-fixture.exe");
    std::fs::copy(env!("CARGO_BIN_EXE_bridge-check"), &program)?;
    let a = std::net::TcpListener::bind("127.0.0.1:0")?;
    let b = std::net::TcpListener::bind("127.0.0.1:0")?;
    let v6 = std::net::TcpListener::bind("[::1]:0")?;
    fn probe(p: &std::path::Path, ip: &str, port: u16) -> Result<bool> {
        let r = platform::output(std::process::Command::new(p).args([
            "tcp-probe",
            ip,
            &port.to_string(),
        ]))?;
        assert!(
            r.status.success(),
            "probe exit {:?}, stderr {}",
            r.status,
            String::from_utf8_lossy(&r.stderr)
        );
        let v: serde_json::Value = serde_json::from_slice(&r.stdout)?;
        Ok(v["connected"].as_bool().unwrap())
    }
    assert!(probe(&program, "127.0.0.1", b.local_addr()?.port())?);
    platform::install_guard(dir.path(), &program, a.local_addr()?.port())?;
    struct Cleanup(std::path::PathBuf);
    impl Drop for Cleanup {
        fn drop(&mut self) {
            let _ = platform::remove_guards(&self.0);
        }
    }
    let _cleanup = Cleanup(dir.path().into());
    assert!(platform::guard_present(
        dir.path(),
        &program,
        a.local_addr()?.port()
    ));
    assert!(
        probe(&program, "127.0.0.1", a.local_addr()?.port())?,
        "The proxy port must remain reachable"
    );
    assert!(
        !probe(&program, "127.0.0.1", b.local_addr()?.port())?,
        "A different loopback port must be blocked"
    );
    assert!(
        !probe(&program, "::1", v6.local_addr()?.port())?,
        "IPv6 bypass must be blocked"
    );
    platform::remove_guards(dir.path())?;
    assert!(probe(&program, "127.0.0.1", b.local_addr()?.port())?);
    println!("WFP: correct port allowed; wrong port and IPv6 denied; restoration passed");
    Ok(())
}
