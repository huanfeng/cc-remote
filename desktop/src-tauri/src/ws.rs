// WebSockets for the page (the terminal stream), for the same reasons as
// net.rs: the host refuses an Origin it does not know, and a LAN host may be
// plain ws:. The page's socket (web/src/platform/desktop.ts) opens one here and
// gets its events on a channel: binary frames raw, everything else as JSON.
use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use futures_util::{SinkExt, StreamExt};
use serde::Serialize;
use tauri::ipc::{Channel, InvokeBody, InvokeResponseBody, Request};
use tauri::State;
use tokio::sync::mpsc;
use tokio_tungstenite::tungstenite::protocol::frame::coding::CloseCode;
use tokio_tungstenite::tungstenite::protocol::CloseFrame;
use tokio_tungstenite::tungstenite::Message;

enum Command {
    Send(Message),
    Close(u16, String),
}

type Open = Arc<Mutex<HashMap<u64, mpsc::UnboundedSender<Command>>>>;

#[derive(Default)]
pub struct Sockets {
    next: AtomicU64,
    open: Open,
}

#[derive(Serialize)]
#[serde(tag = "type", rename_all = "camelCase")]
enum Event {
    Open,
    Text { data: String },
    Close { code: u16, reason: String },
}

fn emit(channel: &Channel, event: Event) {
    if let Ok(json) = serde_json::to_string(&event) {
        let _ = channel.send(InvokeResponseBody::Json(json));
    }
}

#[tauri::command]
pub fn ws_open(url: String, events: Channel, sockets: State<'_, Sockets>) -> Result<u64, String> {
    let parsed = reqwest::Url::parse(&url).map_err(|e| e.to_string())?;
    if parsed.scheme() != "ws" && parsed.scheme() != "wss" {
        return Err(format!("unsupported address: {url}"));
    }
    let id = sockets.next.fetch_add(1, Ordering::Relaxed) + 1;
    let (tx, rx) = mpsc::unbounded_channel::<Command>();
    sockets.open.lock().unwrap().insert(id, tx);
    let open = sockets.open.clone();
    tauri::async_runtime::spawn(async move {
        let (code, reason) = run(&url, &events, rx).await;
        open.lock().unwrap().remove(&id);
        emit(&events, Event::Close { code, reason });
    });
    Ok(id)
}

/// One connection, until it ends; returns the close code and reason to report.
async fn run(url: &str, events: &Channel, mut rx: mpsc::UnboundedReceiver<Command>) -> (u16, String) {
    let stream = match tokio::time::timeout(Duration::from_secs(20), tokio_tungstenite::connect_async(url)).await {
        Ok(Ok((stream, _))) => stream,
        Ok(Err(err)) => return (1006, err.to_string()),
        Err(_) => return (1006, "timed out".into()),
    };
    emit(events, Event::Open);
    let (mut write, mut read) = stream.split();
    let mut closing = false;
    loop {
        tokio::select! {
            command = rx.recv(), if !closing => {
                let message = match command {
                    Some(Command::Send(message)) => message,
                    Some(Command::Close(code, reason)) => {
                        closing = true;
                        Message::Close(Some(CloseFrame { code: CloseCode::from(code), reason: reason.into() }))
                    }
                    None => {
                        closing = true;
                        Message::Close(None)
                    }
                };
                if write.send(message).await.is_err() {
                    return (1006, String::new());
                }
            }
            message = read.next() => match message {
                Some(Ok(Message::Binary(bytes))) => {
                    let _ = events.send(InvokeResponseBody::Raw(bytes.to_vec()));
                }
                Some(Ok(Message::Text(text))) => emit(events, Event::Text { data: text.to_string() }),
                Some(Ok(Message::Close(frame))) => {
                    return frame.map_or((1005, String::new()), |f| (u16::from(f.code), f.reason.to_string()));
                }
                Some(Ok(_)) => {}
                Some(Err(err)) => return (1006, err.to_string()),
                None => return (1006, String::new()),
            },
        }
    }
}

fn sender(sockets: &Sockets, id: u64, command: Command) -> Result<(), String> {
    let open = sockets.open.lock().unwrap();
    let tx = open.get(&id).ok_or("socket closed")?;
    tx.send(command).map_err(|_| "socket closed".to_string())
}

/// A binary frame: the invoke body is the frame, the socket id is the `x-cc-socket` header.
#[tauri::command]
pub fn ws_send(request: Request<'_>, sockets: State<'_, Sockets>) -> Result<(), String> {
    let id: u64 = request
        .headers()
        .get("x-cc-socket")
        .and_then(|v| v.to_str().ok())
        .and_then(|v| v.parse().ok())
        .ok_or("missing socket id")?;
    let bytes = match request.body() {
        InvokeBody::Raw(bytes) => bytes.clone(),
        InvokeBody::Json(_) => return Err("expected a binary frame".into()),
    };
    sender(&sockets, id, Command::Send(Message::Binary(bytes.into())))
}

#[tauri::command]
pub fn ws_send_text(id: u64, text: String, sockets: State<'_, Sockets>) -> Result<(), String> {
    sender(&sockets, id, Command::Send(Message::Text(text.into())))
}

#[tauri::command]
pub fn ws_close(id: u64, code: Option<u16>, reason: Option<String>, sockets: State<'_, Sockets>) {
    let _ = sender(&sockets, id, Command::Close(code.unwrap_or(1000), reason.unwrap_or_default()));
}
