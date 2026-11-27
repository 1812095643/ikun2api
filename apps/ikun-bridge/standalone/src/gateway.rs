use axum::{
    body::{Body, Bytes},
    http::{header, HeaderMap, HeaderValue, Method, StatusCode, Uri},
    response::{IntoResponse, Response},
    Json,
};
use reqwest::{Client, Url};
use serde::Deserialize;
use serde_json::json;
use std::time::{Duration, Instant};
use tokio::sync::Mutex;
use zeroize::Zeroizing;

const PASS_HEADER: &str = "x-ikun-bridge-pass";
const SESSION_PATH: &str = "v1/ikun/bridge/session";
const RETRY_DELAY: Duration = Duration::from_secs(12);

pub struct Gateway {
    pub base: Url,
    key: Zeroizing<String>,
    client: Client,
    ticket: Mutex<Session>,
}

struct Session {
    pass: Option<Pass>,
    next_try: Instant,
    legacy: bool,
    error: Option<String>,
    admission_verified: bool,
    admission_detail: String,
}

struct Pass {
    value: Zeroizing<String>,
    expires: Instant,
}

#[derive(Deserialize)]
struct Issued {
    ticket: String,
    expires_in: u64,
}

pub fn error(status: StatusCode, message: impl AsRef<str>) -> Response {
    (
        status,
        Json(json!({"type":"error","error":{"type":"api_error","message":message.as_ref()}})),
    )
        .into_response()
}

fn base_url(input: &str) -> Result<Url, String> {
    let input = input.trim().trim_end_matches('/');
    let input = input.strip_suffix("/v1").unwrap_or(input);
    let u = Url::parse(&format!("{input}/")).map_err(|_| "网关地址无效")?;
    if u.scheme() != "https"
        || u.host_str().is_none()
        || !u.username().is_empty()
        || u.password().is_some()
        || u.query().is_some()
        || u.fragment().is_some()
        || matches!(
            u.host_str(),
            Some("api.anthropic.com" | "api.claude.ai" | "claude.ai")
        )
    {
        return Err("请输入 IKUN2api 的 HTTPS 网关地址".into());
    }
    Ok(u)
}

pub(crate) fn normalize_gateway(input: &str) -> Result<String, String> {
    Ok(base_url(input)?.as_str().trim_end_matches('/').to_owned())
}

pub(crate) fn validate_credential(key: &str) -> Result<(), String> {
    if key.is_empty() || key.len() > 512 || !key.bytes().all(|b| b.is_ascii_graphic()) {
        return Err("API Key 格式无效".into());
    }
    Ok(())
}

impl Gateway {
    #[cfg(test)]
    pub(crate) fn local_fixture(base: Url, key: String) -> Self {
        assert!(matches!(base.host_str(), Some("127.0.0.1" | "localhost")));
        Self::with_url(base, key).unwrap()
    }
    pub fn new(input: &str, key: String) -> Result<Self, String> {
        Self::with_url(base_url(input)?, key)
    }

    fn with_url(base: Url, key: String) -> Result<Self, String> {
        let key = Zeroizing::new(key);
        validate_credential(&key)?;
        let client = Client::builder()
            .no_proxy()
            .redirect(reqwest::redirect::Policy::none())
            .connect_timeout(Duration::from_secs(10))
            .read_timeout(Duration::from_secs(300))
            .build()
            .map_err(|_| "无法初始化网关连接")?;
        Ok(Self {
            base,
            key,
            client,
            ticket: Mutex::new(Session {
                pass: None,
                next_try: Instant::now(),
                legacy: false,
                error: None,
                admission_verified: false,
                admission_detail: "尚未校验网关准入".into(),
            }),
        })
    }

    pub async fn status(&self) -> (String, Option<String>) {
        let s = self.ticket.lock().await;
        let good = s.pass.as_ref().is_some_and(|p| p.expires > Instant::now());
        let label = if good {
            "已领取"
        } else if s.legacy {
            "旧版网关"
        } else if s.error.is_some() {
            "获取失败"
        } else {
            "待检查"
        };
        (label.to_owned(), s.error.clone())
    }

    pub async fn admission(&self) -> (bool, String) {
        let session = self.ticket.lock().await;
        let current = session
            .pass
            .as_ref()
            .is_some_and(|pass| pass.expires > Instant::now());
        (
            session.admission_verified && current,
            if current {
                session.admission_detail.clone()
            } else {
                "通行证尚未就绪，请检查连接".into()
            },
        )
    }

    // 模型目录查询不发起推理，也不计入客户端接管计数。领到证不等于实际请求已被网关放行。
    pub async fn verify_access(&self) -> Result<bool, String> {
        let ticket = self.refresh(false).await?;
        let Some(ticket) = ticket else {
            return Ok(false);
        };
        let response = self
            .client
            .get(self.base.join("v1/models").expect("fixed path"))
            .header("x-api-key", self.key.as_str())
            .header(PASS_HEADER, ticket)
            .timeout(Duration::from_secs(10))
            .send()
            .await;
        let (verified, detail, denied) = match response {
            Ok(response) if response.status().is_success() => (
                true,
                "网关已接受带通行证的目录查询（不调用模型）".to_owned(),
                false,
            ),
            Ok(response) => {
                let status = response.status();
                if status == StatusCode::FORBIDDEN {
                    let mut response = response;
                    let mut bytes = Vec::new();
                    while let Ok(Some(part)) = response.chunk().await {
                        if bytes.len() + part.len() > 8192 {
                            break;
                        }
                        bytes.extend_from_slice(&part);
                    }
                    let code = serde_json::from_slice::<serde_json::Value>(&bytes)
                        .ok()
                        .and_then(|v| v["error"]["code"].as_str().map(str::to_owned));
                    let denied = code
                        .as_deref()
                        .is_some_and(|c| matches!(c, "bridge_required" | "bridge_expired"));
                    (
                        false,
                        if denied {
                            "网关未接受 Bridge 通行证，请核对站点版本及中间代理是否保留 X-Ikun-Bridge-Pass".into()
                        } else {
                            "网关目录查询返回 403，请检查密钥权限或网站错误记录".into()
                        },
                        denied,
                    )
                } else {
                    (
                        false,
                        format!("目录查询返回 {}，实际模型请求准入尚未验证", status.as_u16()),
                        false,
                    )
                }
            }
            Err(_) => (false, "网关准入校验未完成，请检查连接后重试".into(), false),
        };
        let mut session = self.ticket.lock().await;
        session.admission_verified = verified;
        session.admission_detail = detail.clone();
        if denied {
            Err(detail)
        } else {
            Ok(verified)
        }
    }

    pub async fn refresh(&self, force: bool) -> Result<Option<HeaderValue>, String> {
        let mut s = self.ticket.lock().await;
        let now = Instant::now();
        if force || now >= s.next_try {
            // Reuse the session already issued while refreshing; a transient
            // renewal failure never drops a ticket that is still valid.
            let fetched = self.issue().await;
            let now = Instant::now();
            match fetched {
                Ok(Some(ticket)) => {
                    let ttl = Duration::from_secs(ticket.expires_in);
                    s.pass = Some(Pass {
                        value: Zeroizing::new(ticket.ticket),
                        expires: now + ttl,
                    });
                    s.next_try = now + ttl.saturating_sub(Duration::from_secs(45));
                    s.legacy = false;
                    s.error = None;
                }
                Ok(None) => {
                    s.pass = None;
                    s.next_try = now + Duration::from_secs(300);
                    s.legacy = true;
                    s.error = None;
                }
                Err(reason) => {
                    s.next_try = now + RETRY_DELAY;
                    s.legacy = false;
                    s.error = Some(reason);
                }
            }
        }
        if let Some(p) = &s.pass {
            if Instant::now() < p.expires {
                let mut value =
                    HeaderValue::from_str(p.value.as_str()).map_err(|_| "通行证格式无效")?;
                value.set_sensitive(true);
                return Ok(Some(value));
            }
        }
        if s.legacy {
            Ok(None)
        } else {
            Err(s.error.clone().unwrap_or_else(|| "通行证已过期".into()))
        }
    }

    async fn issue(&self) -> Result<Option<Issued>, String> {
        let t = Instant::now();
        let r = self
            .client
            .post(self.base.join(SESSION_PATH).expect("fixed relative path"))
            .timeout(Duration::from_secs(10))
            .header("x-api-key", self.key.as_str())
            .send()
            .await
            .map_err(|_| "连接网关失败")?;
        match r.status() {
            StatusCode::NOT_FOUND | StatusCode::METHOD_NOT_ALLOWED => return Ok(None),
            StatusCode::UNAUTHORIZED | StatusCode::FORBIDDEN => {
                return Err(format!("网关拒绝 API Key（{}）", r.status().as_u16()))
            }
            StatusCode::OK => {}
            s => return Err(format!("领取通行证失败（{}）", s.as_u16())),
        }
        // A session response is tiny. Reject oversized/chunked surprises without
        // buffering a gateway error page or an unbounded response.
        let mut r = r;
        let mut bytes = Zeroizing::new(Vec::with_capacity(256));
        while let Some(part) = r.chunk().await.map_err(|_| "无法读取通行证")? {
            if bytes.len() + part.len() > 2048 {
                return Err("通行证响应过大".into());
            }
            bytes.extend_from_slice(&part);
        }
        let issued: Issued = serde_json::from_slice(&bytes).map_err(|_| "通行证响应无效")?;
        if !(60..=300).contains(&issued.expires_in)
            || issued.ticket.len() != 66
            || !issued
                .ticket
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'-' | b'_'))
        {
            return Err("通行证格式无效".into());
        }
        let expires_in = issued
            .expires_in
            .saturating_sub(t.elapsed().as_secs().saturating_add(2));
        if expires_in <= 45 {
            return Err("领取通行证超时".into());
        }
        Ok(Some(Issued {
            expires_in,
            ..issued
        }))
    }

    pub async fn forward(
        &self,
        method: Method,
        uri: Uri,
        headers: HeaderMap,
        body: Bytes,
    ) -> Response {
        let path = uri.path();
        if !allowed(&method, path) {
            return error(StatusCode::NOT_FOUND, "此接口未启用");
        }
        let ticket = match self.refresh(false).await {
            Ok(ticket) => ticket,
            Err(reason) => return error(StatusCode::SERVICE_UNAVAILABLE, reason),
        };
        let mut address = self.base.join(&path[1..]).expect("validated API path");
        address.set_query(uri.query());
        let mut req = self
            .client
            .request(method, address)
            .header("x-api-key", self.key.as_str())
            .body(body);
        if let Some(ticket) = ticket {
            req = req.header(PASS_HEADER, ticket);
        }
        for name in [
            "anthropic-version",
            "anthropic-beta",
            "content-type",
            "accept",
            "user-agent",
            "x-app",
        ] {
            for value in headers.get_all(name) {
                req = req.header(name, value);
            }
        }
        let res = match req.send().await {
            Ok(r) if !r.status().is_redirection() => r,
            Ok(_) => return error(StatusCode::BAD_GATEWAY, "网关返回重定向"),
            Err(_) => return error(StatusCode::BAD_GATEWAY, "网关请求失败"),
        };
        let mut out = Response::builder()
            .status(res.status())
            .header(header::CACHE_CONTROL, "no-store");
        for name in ["content-type", "retry-after", "request-id", "x-request-id"] {
            if let Some(v) = res.headers().get(name) {
                out = out.header(name, v);
            }
        }
        out.body(Body::from_stream(res.bytes_stream()))
            .unwrap_or_else(|_| error(StatusCode::BAD_GATEWAY, "网关响应无效"))
    }
}

fn allowed(method: &Method, path: &str) -> bool {
    match *method {
        Method::POST => matches!(path, "/v1/messages" | "/v1/messages/count_tokens"),
        Method::GET => {
            path == "/v1/models"
                || path.strip_prefix("/v1/models/").is_some_and(|id| {
                    !id.is_empty()
                        && id != "."
                        && id != ".."
                        && id.len() <= 128
                        && id.bytes().all(|c| {
                            c.is_ascii_alphanumeric() || matches!(c, b'-' | b'_' | b'.' | b':')
                        })
                })
        }
        _ => false,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::{routing::post, Router};
    use std::sync::{
        atomic::{AtomicUsize, Ordering},
        Arc,
    };

    #[tokio::test]
    async fn session_is_cached_and_owns_outbound_credentials() {
        let issued = Arc::new(AtomicUsize::new(0));
        let mint_calls = issued.clone();
        let router = Router::new()
            .route(
                "/v1/ikun/bridge/session",
                post(move |headers: HeaderMap| {
                    let calls = mint_calls.clone();
                    async move {
                        assert_eq!(headers["x-api-key"], "fixture-api-key");
                        calls.fetch_add(1, Ordering::Relaxed);
                        Json(json!({"ticket": "A".repeat(66), "expires_in": 300}))
                    }
                }),
            )
            .route(
                "/v1/messages",
                post(|headers: HeaderMap, body: Bytes| async move {
                    assert_eq!(headers["x-api-key"], "fixture-api-key");
                    assert_eq!(headers[PASS_HEADER], "A".repeat(66).as_str());
                    assert_eq!(headers["anthropic-version"], "2023-06-01");
                    assert_eq!(
                        headers["user-agent"],
                        "claude-cli/2.1.326 (external, claude-desktop-3p)"
                    );
                    assert_eq!(headers["x-app"], "cli");
                    assert!(!headers.contains_key("authorization"));
                    assert!(!headers.contains_key("x-test-client-private"));
                    assert_eq!(
                        body,
                        r#"{"messages":[{"role":"user","content":"hello"}],"stream":true}"#
                    );
                    (
                        [(header::CONTENT_TYPE, "text/event-stream")],
                        "event: message_stop\ndata: {}\n\n",
                    )
                }),
            );
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let base = Url::parse(&format!("http://{}/", listener.local_addr().unwrap())).unwrap();
        let server = tokio::spawn(async move { axum::serve(listener, router).await.unwrap() });
        let gateway = Gateway::with_url(base, "fixture-api-key".into()).unwrap();
        assert!(gateway.refresh(true).await.unwrap().is_some());
        for _ in 0..2 {
            let mut headers = HeaderMap::new();
            headers.insert(
                "authorization",
                HeaderValue::from_static("Bearer local-key"),
            );
            headers.insert("anthropic-version", HeaderValue::from_static("2023-06-01"));
            headers.insert(
                "user-agent",
                HeaderValue::from_static("claude-cli/2.1.326 (external, claude-desktop-3p)"),
            );
            headers.insert("x-app", HeaderValue::from_static("cli"));
            headers.insert(PASS_HEADER, HeaderValue::from_static("ignored"));
            headers.insert("x-test-client-private", HeaderValue::from_static("ignored"));
            let response = gateway
                .forward(
                    Method::POST,
                    Uri::from_static("/v1/messages"),
                    headers,
                    Bytes::from_static(
                        br#"{"messages":[{"role":"user","content":"hello"}],"stream":true}"#,
                    ),
                )
                .await;
            assert_eq!(response.status(), StatusCode::OK);
            let payload = axum::body::to_bytes(response.into_body(), 128)
                .await
                .unwrap();
            assert_eq!(payload, "event: message_stop\ndata: {}\n\n");
        }
        assert_eq!(issued.load(Ordering::Relaxed), 1);
        server.abort();
    }

    #[test]
    fn gateway_requires_an_explicit_https_origin() {
        assert_eq!(
            base_url("https://gateway.example.test/v1/")
                .unwrap()
                .as_str(),
            "https://gateway.example.test/"
        );
        for url in [
            "http://gateway.example.test",
            "https://api.anthropic.com",
            "https://user@gateway.example.test",
            "https://gateway.example.test?key=x",
        ] {
            assert!(base_url(url).is_err());
        }
        assert!(!allowed(&Method::GET, "/v1/models/.."));
        assert!(!allowed(&Method::POST, "/api/event_logging/batch"));
    }

    #[tokio::test]
    async fn issued_ticket_does_not_claim_model_route_admission() {
        let router = Router::new()
            .route(
                "/v1/ikun/bridge/session",
                post(|| async { Json(json!({"ticket":"A".repeat(66),"expires_in":300})) }),
            )
            .route(
                "/v1/models",
                axum::routing::get(|| async {
                    (
                        StatusCode::FORBIDDEN,
                        Json(json!({"error":{"code":"bridge_required"}})),
                    )
                }),
            );
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let base = Url::parse(&format!("http://{}/", listener.local_addr().unwrap())).unwrap();
        let server = tokio::spawn(async move { axum::serve(listener, router).await.unwrap() });
        let gateway = Gateway::with_url(base, "fixture-key".into()).unwrap();
        assert!(gateway.refresh(true).await.unwrap().is_some());
        assert_eq!(gateway.status().await.0, "已领取");
        assert!(gateway.verify_access().await.is_err());
        assert!(!gateway.admission().await.0);
        server.abort();
    }
}
