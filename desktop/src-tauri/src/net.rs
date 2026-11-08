// HTTP for the page. The bundled frontend talks to hosts it does not share an
// origin with; the host refuses a browser request whose Origin it does not know
// (internal/server/server.go), and a host on the LAN is often plain http. So
// the page's fetch comes here (web/src/platform/desktop.ts) and goes out from
// Rust: no Origin header, no CORS, any http(s) address.
//
// Wire format, raw both ways so a file or attachment is not turned into JSON:
// the request's body is the invoke body and its method, URL and headers ride in
// the `x-cc-meta` header (percent-encoded JSON). The reply is
// [meta length: u32 big-endian][meta JSON][body bytes].
use std::collections::HashMap;
use std::sync::Mutex;
use std::time::Duration;

use percent_encoding::percent_decode_str;
use serde::{Deserialize, Serialize};
use tauri::ipc::{InvokeBody, Request, Response};
use tauri::State;
use tokio::sync::oneshot;

pub struct Net {
    client: reqwest::Client,
    inflight: Mutex<HashMap<u64, oneshot::Sender<()>>>,
}

impl Net {
    pub fn new() -> Self {
        let client = reqwest::Client::builder()
            .connect_timeout(Duration::from_secs(15))
            .user_agent(concat!("cc-remote-desktop/", env!("CARGO_PKG_VERSION")))
            .build()
            .expect("HTTP client");
        Net { client, inflight: Mutex::new(HashMap::new()) }
    }
}

#[derive(Deserialize)]
struct Meta {
    /// Lets the page abort the request (http_abort).
    id: u64,
    method: String,
    url: String,
    #[serde(default)]
    headers: Vec<(String, String)>,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct ReplyMeta {
    status: u16,
    status_text: String,
    url: String,
    headers: Vec<(String, String)>,
}

/// Headers the page must not set: the browser owns them, and an Origin would
/// make the host refuse the request.
fn forbidden(name: &str) -> bool {
    matches!(
        name.to_ascii_lowercase().as_str(),
        "origin" | "host" | "content-length" | "connection" | "cookie" | "referer" | "transfer-encoding"
    )
}

#[tauri::command]
pub async fn http_request(request: Request<'_>, net: State<'_, Net>) -> Result<Response, String> {
    let raw = request
        .headers()
        .get("x-cc-meta")
        .and_then(|v| v.to_str().ok())
        .ok_or("missing request meta")?;
    let meta: Meta = serde_json::from_str(&percent_decode_str(raw).decode_utf8_lossy()).map_err(|e| e.to_string())?;
    let url = reqwest::Url::parse(&meta.url).map_err(|e| e.to_string())?;
    if url.scheme() != "http" && url.scheme() != "https" {
        return Err(format!("unsupported address: {}", meta.url));
    }
    let method = reqwest::Method::from_bytes(meta.method.as_bytes()).map_err(|e| e.to_string())?;
    let mut builder = net.client.request(method, url);
    for (k, v) in &meta.headers {
        if !forbidden(k) {
            builder = builder.header(k, v);
        }
    }
    let body = match request.body() {
        InvokeBody::Raw(bytes) if !bytes.is_empty() => Some(bytes.clone()),
        _ => None,
    };
    if let Some(bytes) = body {
        builder = builder.body(bytes);
    }

    let (abort, aborted) = oneshot::channel();
    net.inflight.lock().unwrap().insert(meta.id, abort);
    let id = meta.id;
    let result = tokio::select! {
        r = async {
            let response = builder.send().await?;
            let status = response.status();
            let url = response.url().to_string();
            let headers = response
                .headers()
                .iter()
                .filter_map(|(k, v)| v.to_str().ok().map(|v| (k.to_string(), v.to_string())))
                .collect();
            let body = response.bytes().await?;
            Ok::<_, reqwest::Error>((status, url, headers, body))
        } => r.map_err(describe),
        _ = aborted => Err("aborted".to_string()),
    };
    net.inflight.lock().unwrap().remove(&id);
    let (status, url, headers, body) = result?;

    let meta = serde_json::to_vec(&ReplyMeta {
        status: status.as_u16(),
        status_text: status.canonical_reason().unwrap_or("").to_string(),
        url,
        headers,
    })
    .map_err(|e| e.to_string())?;
    let mut out = Vec::with_capacity(4 + meta.len() + body.len());
    out.extend_from_slice(&(meta.len() as u32).to_be_bytes());
    out.extend_from_slice(&meta);
    out.extend_from_slice(&body);
    Ok(Response::new(out))
}

#[tauri::command]
pub fn http_abort(id: u64, net: State<'_, Net>) {
    if let Some(abort) = net.inflight.lock().unwrap().remove(&id) {
        let _ = abort.send(());
    }
}

/// The error with its causes: reqwest's own message alone ("error sending
/// request") does not say whether the name, the connection or TLS failed.
fn describe(err: reqwest::Error) -> String {
    let mut text = err.to_string();
    let mut source = std::error::Error::source(&err);
    while let Some(cause) = source {
        let part = cause.to_string();
        if !text.contains(&part) {
            text.push_str(": ");
            text.push_str(&part);
        }
        source = cause.source();
    }
    text
}
