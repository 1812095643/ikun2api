//! Authenticated loopback-only CONNECT proxy. Never tunnels arbitrary destinations.
use crate::{
    settings::{gateway_url, Settings},
    store::{Secrets, Store},
};
use anyhow::{ensure, Context, Result};
use bytes::Bytes;
use futures_util::TryStreamExt;
use http_body_util::{combinators::UnsyncBoxBody, BodyExt, Full, Limited, StreamBody};
use hyper::{
    body::{Frame, Incoming},
    service::service_fn,
    Method, Request, Response, StatusCode,
};
use hyper_util::rt::{TokioExecutor, TokioIo};
use rand::{rngs::OsRng, RngCore};
use rcgen::{BasicConstraints, CertificateParams, DistinguishedName, DnType, IsCa, KeyPair};
use serde::Serialize;
use std::{
    collections::VecDeque,
    convert::Infallible,
    io::BufReader,
    sync::{
        atomic::{AtomicU64, Ordering},
        Arc, Mutex,
    },
    time::Duration,
};
use subtle::ConstantTimeEq;
use tokio::{net::TcpListener, task::JoinHandle};
use tokio_rustls::{
    rustls::{
        self,
        pki_types::{CertificateDer, PrivateKeyDer},
    },
    TlsAcceptor,
};
use tokio_util::sync::CancellationToken;
type Body = UnsyncBoxBody<Bytes, Box<dyn std::error::Error + Send + Sync>>;
#[derive(Clone, Serialize)]
pub struct Event {
    pub time: u64,
    pub action: String,
    pub path: String,
    pub status: u16,
}
#[derive(Clone, Serialize)]
pub struct Stats {
    pub forwarded: u64,
    pub blocked: u64,
    pub filtered: u64,
    pub events: Vec<Event>,
}
#[derive(Default)]
pub struct Metrics {
    forwarded: AtomicU64,
    blocked: AtomicU64,
    filtered: AtomicU64,
    events: Mutex<VecDeque<Event>>,
}
impl Metrics {
    fn record(&self, action: &str, path: &str, status: u16) {
        let event = Event {
            time: std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap_or_default()
                .as_secs(),
            action: action.into(),
            path: match path {
                "/v1/messages"
                | "/v1/messages/count_tokens"
                | "/v1/models"
                | "/api/claude_code/settings"
                | "/api/claude_code/policy_limits" => path.into(),
                _ => "其他接口".into(),
            },
            status,
        };
        if let Ok(mut q) = self.events.lock() {
            q.push_front(event);
            q.truncate(60);
        }
    }
    pub fn snapshot(&self) -> Stats {
        Stats {
            forwarded: self.forwarded.load(Ordering::Relaxed),
            blocked: self.blocked.load(Ordering::Relaxed),
            filtered: self.filtered.load(Ordering::Relaxed),
            events: self
                .events
                .lock()
                .map(|q| q.iter().cloned().collect())
                .unwrap_or_default(),
        }
    }
}
pub struct RunningProxy {
    pub port: u16,
    pub cancel: CancellationToken,
    pub task: JoinHandle<()>,
    pub metrics: Arc<Metrics>,
    pub nonce: String,
}
impl RunningProxy {
    pub async fn stop(self) {
        self.cancel.cancel();
        let _ = self.task.await;
    }
    pub fn alive(&self) -> bool {
        !self.task.is_finished() && !self.cancel.is_cancelled()
    }
}
struct ContextState {
    client: reqwest::Client,
    gateway: url::Url,
    token: String,
    key: String,
    tls: TlsAcceptor,
    stop: CancellationToken,
    metrics: Arc<Metrics>,
    nonce: String,
}
impl Drop for ContextState {
    fn drop(&mut self) {
        use zeroize::Zeroize;
        self.token.zeroize();
        self.key.zeroize();
    }
}
fn full(status: StatusCode, text: impl Into<Bytes>) -> Response<Body> {
    Response::builder()
        .status(status)
        .header("content-type", "application/json")
        .header("cache-control", "no-store")
        .body(
            Full::new(text.into())
                .map_err(|e: Infallible| match e {})
                .boxed_unsync(),
        )
        .unwrap()
}
fn error(status: StatusCode, message: &str) -> Response<Body> {
    full(
        status,
        serde_json::to_vec(
            &serde_json::json!({"type":"error","error":{"type":"bridge_error","message":message}}),
        )
        .unwrap(),
    )
}

pub fn ensure_certificates(store: &Store, secrets: &mut Secrets) -> Result<()> {
    if secrets.ca_cert.is_empty() || secrets.ca_key.is_empty() {
        let mut root = CertificateParams::new(vec![])?;
        root.is_ca = IsCa::Ca(BasicConstraints::Constrained(0));
        root.distinguished_name = DistinguishedName::new();
        let mut suffix = [0; 4];
        OsRng.fill_bytes(&mut suffix);
        root.distinguished_name.push(
            DnType::CommonName,
            format!("IKUN Bridge Local CA {}", hex::encode(suffix)),
        );
        root.key_usages = vec![
            rcgen::KeyUsagePurpose::KeyCertSign,
            rcgen::KeyUsagePurpose::CrlSign,
        ];
        let key = KeyPair::generate()?;
        let cert = root.self_signed(&key)?;
        secrets.ca_key = key.serialize_pem();
        secrets.ca_cert = cert.pem();
    }
    let key = KeyPair::from_pem(&secrets.ca_key)?;
    let ca = CertificateParams::from_ca_cert_pem(&secrets.ca_cert)?.self_signed(&key)?;
    let mut leaf = CertificateParams::new(vec!["api.anthropic.com".into()])?;
    leaf.distinguished_name
        .push(DnType::CommonName, "api.anthropic.com");
    leaf.extended_key_usages = vec![rcgen::ExtendedKeyUsagePurpose::ServerAuth];
    let leaf_key = KeyPair::generate()?;
    let cert = leaf.signed_by(&leaf_key, &ca, &key)?;
    secrets.leaf_cert = cert.pem();
    secrets.leaf_key = leaf_key.serialize_pem();
    store.save_secrets(secrets)?;
    crate::config_edit::atomic_write(
        &store.root.join("bridge-ca.pem"),
        secrets.ca_cert.as_bytes(),
    )?;
    Ok(())
}
fn tls(secrets: &Secrets) -> Result<TlsAcceptor> {
    let certs: Vec<CertificateDer<'static>> =
        rustls_pemfile::certs(&mut BufReader::new(secrets.leaf_cert.as_bytes()))
            .collect::<std::io::Result<_>>()?;
    let key: PrivateKeyDer<'static> =
        rustls_pemfile::private_key(&mut BufReader::new(secrets.leaf_key.as_bytes()))?
            .context("缺少 TLS 私钥")?;
    let provider = Arc::new(rustls::crypto::ring::default_provider());
    let mut config = rustls::ServerConfig::builder_with_provider(provider)
        .with_safe_default_protocol_versions()?
        .with_no_client_auth()
        .with_single_cert(certs, key)?;
    config.alpn_protocols = vec![b"h2".to_vec(), b"http/1.1".to_vec()];
    Ok(TlsAcceptor::from(Arc::new(config)))
}
pub async fn start(settings: &Settings, secrets: &Secrets, fixture: bool) -> Result<RunningProxy> {
	let listener = TcpListener::bind((std::net::Ipv4Addr::LOCALHOST, settings.listen_port))
		.await
		.context("本地端口无法绑定，请检查是否被占用")?;
	start_on_listener(settings, secrets, fixture, listener).await
}

// 桌面版复用已经独占绑定的持久端口，避免重启后客户端仍指向旧代理端口。
pub async fn start_on_listener(settings: &Settings, secrets: &Secrets, fixture: bool, listener: TcpListener) -> Result<RunningProxy> {
    let gateway = gateway_url(&settings.gateway_url, fixture)?;
    ensure!(!secrets.gateway_key.is_empty(), "请先保存网关 Key");
    let mut client = reqwest::Client::builder()
        .redirect(reqwest::redirect::Policy::none())
        .connect_timeout(Duration::from_secs(15))
        .read_timeout(Duration::from_secs(180))
        .pool_idle_timeout(Duration::from_secs(90))
        .no_proxy();
    if !settings.upstream_proxy.is_empty() {
        client = client.proxy(reqwest::Proxy::all(&settings.upstream_proxy)?)
    }
    let port = listener.local_addr()?.port();
    let cancel = CancellationToken::new();
    let metrics = Arc::new(Metrics::default());
    let nonce = uuid::Uuid::new_v4().to_string();
    let shared = Arc::new(ContextState {
        client: client.build()?,
        gateway,
        token: secrets.local_token.clone(),
        key: secrets.gateway_key.clone(),
        tls: tls(secrets)?,
        stop: cancel.clone(),
        metrics: metrics.clone(),
        nonce: nonce.clone(),
    });
    let task = tokio::spawn(async move {
        loop {
            let accepted = tokio::select! {_ = shared.stop.cancelled()=>break,result=listener.accept()=>result};
            let Ok((stream, _)) = accepted else { break };
            let s = shared.clone();
            tokio::spawn(async move {
                let connection = hyper::server::conn::http1::Builder::new()
                    .header_read_timeout(Duration::from_secs(10))
                    .timer(hyper_util::rt::TokioTimer::new())
                    .serve_connection(
                        TokioIo::new(stream),
                        service_fn({
                            let s = s.clone();
                            move |req| ingress(req, s.clone())
                        }),
                    )
                    .with_upgrades();
                tokio::select! {_ = s.stop.cancelled()=>{}, _ = connection=>{}}
            });
        }
    });
    Ok(RunningProxy {
        port,
        cancel,
        task,
        metrics,
        nonce,
    })
}
async fn ingress(
    req: Request<Incoming>,
    s: Arc<ContextState>,
) -> Result<Response<Body>, Infallible> {
    if req.method() == Method::GET
        && req.uri().path() == "/bridge/health"
        && req.uri().authority().is_none()
    {
        return Ok(full(StatusCode::OK,serde_json::to_vec(&serde_json::json!({"app":"ikun-bridge","version":env!("CARGO_PKG_VERSION"),"nonce":s.nonce})).unwrap()));
    }
    if req.method() != Method::CONNECT
        || req.uri().authority().map(|a| a.as_str()) != Some("api.anthropic.com:443")
    {
        s.metrics.blocked.fetch_add(1, Ordering::Relaxed);
        s.metrics.record("拦截", req.uri().path(), 403);
        return Ok(error(
            StatusCode::FORBIDDEN,
            "本地服务仅接管官方 API 的指定接口",
        ));
    }
    let upgrade = hyper::upgrade::on(req);
    let s2 = s.clone();
    tokio::spawn(async move {
        let result = tokio::select! {_ = s2.stop.cancelled()=>return,r=tokio::time::timeout(Duration::from_secs(15),upgrade)=>r};
        let Ok(Ok(upgraded)) = result else { return };
        let stream = TokioIo::new(upgraded);
        let result = tokio::select! {_ = s2.stop.cancelled()=>return,r=tokio::time::timeout(Duration::from_secs(15),s2.tls.accept(stream))=>r};
        let Ok(Ok(stream)) = result else { return };
        let builder = hyper_util::server::conn::auto::Builder::new(TokioExecutor::new());
        let conn = builder.serve_connection(
            TokioIo::new(stream),
            service_fn({
                let s = s2.clone();
                move |req| model(req, s.clone())
            }),
        );
        tokio::select! {_ = s2.stop.cancelled()=>{}, _=conn=>{}}
    });
    Ok(full(StatusCode::OK, Bytes::new()))
}
fn authorized(req: &Request<Incoming>, token: &str) -> bool {
    let actual = req
        .headers()
        .get("x-api-key")
        .and_then(|h| h.to_str().ok())
        .or_else(|| {
            req.headers()
                .get("authorization")
                .and_then(|h| h.to_str().ok())
                .and_then(|h| h.strip_prefix("Bearer "))
        });
    actual.is_some_and(|a| bool::from(a.as_bytes().ct_eq(token.as_bytes())))
}
fn endpoint(base: &url::Url, path: &str) -> Result<url::Url> {
    let mut url = base.clone();
    let prefix = base.path().trim_end_matches('/');
    let prefix = prefix.strip_suffix("/v1").unwrap_or(prefix);
    url.set_path(&format!("{prefix}{path}"));
    url.set_query(None);
    url.set_fragment(None);
    Ok(url)
}
async fn model(req: Request<Incoming>, s: Arc<ContextState>) -> Result<Response<Body>, Infallible> {
    let path = req.uri().path().to_string();
    let allowed = matches!(
        (req.method(), path.as_str()),
        (&Method::POST, "/v1/messages" | "/v1/messages/count_tokens")
            | (&Method::GET, "/v1/models")
    ) || req.method() == Method::GET && path.strip_prefix("/v1/models/").is_some_and(|id| {
        !id.is_empty() && id != "." && id != ".." && id.len() <= 128
            && id.bytes().all(|c| c.is_ascii_alphanumeric() || matches!(c, b'-' | b'_' | b'.' | b':'))
    });
    if !allowed {
        s.metrics.blocked.fetch_add(1, Ordering::Relaxed);
        s.metrics.record("拦截", &path, 403);
        return Ok(error(StatusCode::FORBIDDEN, "此接口未由 IKUN Bridge 放行"));
    }
    if !authorized(&req, &s.token) {
        s.metrics.blocked.fetch_add(1, Ordering::Relaxed);
        return Ok(error(
            StatusCode::UNAUTHORIZED,
            "本地授权标识无效，请重新通过 IKUN Bridge 启动",
        ));
    }
    if req
        .headers()
        .get("content-encoding")
        .is_some_and(|h| h != "identity")
    {
        return Ok(error(
            StatusCode::UNSUPPORTED_MEDIA_TYPE,
            "不接受压缩的请求正文",
        ));
    }
    let (parts, body) = req.into_parts();
    let raw = match tokio::time::timeout(
        Duration::from_secs(30),
        Limited::new(body, 32 * 1024 * 1024).collect(),
    )
    .await
    {
        Ok(Ok(b)) => b.to_bytes(),
        _ => return Ok(error(StatusCode::PAYLOAD_TOO_LARGE, "请求过大或读取超时")),
    };
    let clean = if parts.method == Method::POST {
        match crate::privacy::sanitize(&raw) {
            Ok(b) => b,
            Err(_) => return Ok(error(StatusCode::BAD_REQUEST, "请求 JSON 或协议字段无效")),
        }
    } else {
        vec![]
    };
    if clean.as_slice() != raw.as_ref() {
        s.metrics.filtered.fetch_add(1, Ordering::Relaxed);
    }
    let mut url = match endpoint(&s.gateway, &path) {
        Ok(u) => u,
        Err(_) => return Ok(error(StatusCode::BAD_GATEWAY, "网关地址构造失败")),
    };
    if parts.uri.query().is_some_and(|q| {
        url::form_urlencoded::parse(q.as_bytes()).any(|(k, v)| k == "beta" && v == "true")
    }) {
        url.set_query(Some("beta=true"))
    }
    let mut request = s
        .client
        .request(parts.method, url)
        .header("x-api-key", &s.key)
        .header("authorization", format!("Bearer {}", s.key))
        .header("content-type", "application/json")
        .header("accept-encoding", "identity");
    for (k, v) in &parts.headers {
        if crate::privacy::allowed_header(k.as_str()) {
            if k == "anthropic-beta" {
                if let Ok(v) = v.to_str() {
                    let b = crate::privacy::clean_beta(v);
                    if !b.is_empty() {
                        request = request.header(k, b);
                    }
                }
            } else {
                request = request.header(k, v);
            }
        }
    }
    let upstream = tokio::select! {_ = s.stop.cancelled()=>return Ok(error(StatusCode::SERVICE_UNAVAILABLE,"接管已停止")),r=request.body(clean).send()=>r};
    let upstream = match upstream {
        Ok(r) => r,
        Err(_) => {
            s.metrics.record("网关异常", &path, 502);
            return Ok(error(
                StatusCode::BAD_GATEWAY,
                "连接网关失败，请检查网关与出口代理",
            ));
        }
    };
    let status = upstream.status();
    if status.is_redirection() {
        s.metrics.record("拒绝重定向", &path, 502);
        return Ok(error(
            StatusCode::BAD_GATEWAY,
            "网关重定向已拒绝，未转发密钥",
        ));
    }
    s.metrics.forwarded.fetch_add(1, Ordering::Relaxed);
    s.metrics.record("已转发", &path, status.as_u16());
    let mut response = Response::builder()
        .status(status)
        .header("cache-control", "no-store");
    for k in [
        "content-type",
        "request-id",
        "retry-after",
        "anthropic-ratelimit-requests-remaining",
        "anthropic-ratelimit-tokens-remaining",
    ] {
        if let Some(v) = upstream.headers().get(k) {
            response = response.header(k, v)
        }
    }
    let stream = upstream
        .bytes_stream()
        .map_ok(Frame::data)
        .map_err(|e| Box::new(e) as Box<dyn std::error::Error + Send + Sync>);
    Ok(response
        .body(StreamBody::new(stream).boxed_unsync())
        .unwrap())
}
pub async fn health(port: u16, nonce: &str) -> bool {
    let result = reqwest::Client::builder()
        .no_proxy()
        .timeout(Duration::from_secs(2))
        .build()
        .unwrap()
        .get(format!("http://127.0.0.1:{port}/bridge/health"))
        .send()
        .await;
    match result {
        Ok(r) => r
            .json::<serde_json::Value>()
            .await
            .ok()
            .is_some_and(|v| v["nonce"] == nonce && v["app"] == "ikun-bridge"),
        Err(_) => false,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn gateway_prefix_and_key_isolation() {
        let u = url::Url::parse("https://gateway.example/tenant/v1").unwrap();
        assert_eq!(
            endpoint(&u, "/v1/messages").unwrap().as_str(),
            "https://gateway.example/tenant/v1/messages"
        );
        assert!(!crate::privacy::allowed_header("x-claude-code-wiggly-dove"));
        assert!(!crate::privacy::allowed_header("authorization"));
    }
}
