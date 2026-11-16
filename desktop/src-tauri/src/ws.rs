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
use tokio::sync::{mpsc, oneshot};
use tokio_tungstenite::tungstenite::protocol::frame::coding::CloseCode;
use tokio_tungstenite::tungstenite::protocol::CloseFrame;
use tokio_tungstenite::tungstenite::Message;

enum Command {
    Send(Message),
    Close(u16, String),
}

struct OwnedSocket {
    commands: mpsc::UnboundedSender<Command>,
    // Dropping this sender cancels both a pending handshake and an open socket.
    _cancel: oneshot::Sender<()>,
}

type Open = Arc<Mutex<HashMap<(String, u64), OwnedSocket>>>;

#[derive(Default)]
pub struct Sockets {
    next: AtomicU64,
    open: Open,
}

impl Sockets {
    pub fn close_window(&self, label: &str) {
        self.open
            .lock()
            .unwrap()
            .retain(|(owner, _), _| owner != label && !crate::tabs::owns(label, owner));
    }

    pub fn close_view(&self, label: &str) {
        self.open
            .lock()
            .unwrap()
            .retain(|(owner, _), _| owner != label);
    }
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
pub fn ws_open<R: tauri::Runtime>(
    webview: tauri::Webview<R>,
    url: String,
    events: Channel,
    sockets: State<'_, Sockets>,
) -> Result<u64, String> {
    let parsed = reqwest::Url::parse(&url).map_err(|e| e.to_string())?;
    if parsed.scheme() != "ws" && parsed.scheme() != "wss" {
        return Err(format!("unsupported address: {url}"));
    }
    Ok(start(&sockets, webview.label().to_string(), url, events))
}

fn start(sockets: &Sockets, owner: String, url: String, events: Channel) -> u64 {
    let id = sockets.next.fetch_add(1, Ordering::Relaxed) + 1;
    let (tx, rx) = mpsc::unbounded_channel::<Command>();
    let (cancel, cancelled) = oneshot::channel();
    let key = (owner, id);
    sockets.open.lock().unwrap().insert(
        key.clone(),
        OwnedSocket {
            commands: tx,
            _cancel: cancel,
        },
    );
    let open = sockets.open.clone();
    let event_owners = open.clone();
    let event_key = key.clone();
    // A page can reload and reuse JS callback ids before a task gets polled to
    // observe cancellation. Stop all old channel sends as soon as ownership ends.
    let guarded_events = Channel::new(move |body| {
        let owners = event_owners.lock().unwrap();
        if owners.contains_key(&event_key) {
            events.send(body)
        } else {
            Ok(())
        }
    });
    tauri::async_runtime::spawn(async move {
        let finished = tokio::select! {
            biased;
            _ = cancelled => None,
            result = run(&url, &guarded_events, rx) => Some(result),
        };
        if let Some((code, reason)) = finished {
            emit(&guarded_events, Event::Close { code, reason });
        }
        open.lock().unwrap().remove(&key);
    });
    id
}

/// One connection, until it ends; returns the close code and reason to report.
async fn run(
    url: &str,
    events: &Channel,
    mut rx: mpsc::UnboundedReceiver<Command>,
) -> (u16, String) {
    let stream = match tokio::time::timeout(
        Duration::from_secs(20),
        tokio_tungstenite::connect_async(url),
    )
    .await
    {
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

fn sender(sockets: &Sockets, owner: &str, id: u64, command: Command) -> Result<(), String> {
    let open = sockets.open.lock().unwrap();
    let tx = open.get(&(owner.to_string(), id)).ok_or("socket closed")?;
    tx.commands
        .send(command)
        .map_err(|_| "socket closed".to_string())
}

/// A binary frame: the invoke body is the frame, the socket id is the `x-cc-socket` header.
#[tauri::command]
pub fn ws_send<R: tauri::Runtime>(
    webview: tauri::Webview<R>,
    request: Request<'_>,
    sockets: State<'_, Sockets>,
) -> Result<(), String> {
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
    sender(
        &sockets,
        webview.label(),
        id,
        Command::Send(Message::Binary(bytes)),
    )
}

#[tauri::command]
pub fn ws_send_text<R: tauri::Runtime>(
    webview: tauri::Webview<R>,
    id: u64,
    text: String,
    sockets: State<'_, Sockets>,
) -> Result<(), String> {
    sender(
        &sockets,
        webview.label(),
        id,
        Command::Send(Message::Text(text)),
    )
}

#[tauri::command]
pub fn ws_close<R: tauri::Runtime>(
    webview: tauri::Webview<R>,
    id: u64,
    code: Option<u16>,
    reason: Option<String>,
    sockets: State<'_, Sockets>,
) {
    let _ = sender(
        &sockets,
        webview.label(),
        id,
        Command::Close(code.unwrap_or(1000), reason.unwrap_or_default()),
    );
}

#[cfg(test)]
mod tests {
    use super::*;

    fn owned(commands: mpsc::UnboundedSender<Command>) -> OwnedSocket {
        let (cancel, _) = oneshot::channel();
        OwnedSocket {
            commands,
            _cancel: cancel,
        }
    }

    #[test]
    fn disposing_tab_cancels_pending_handshake_without_sending_old_channel_events() {
        use tokio::io::AsyncReadExt;
        let runtime = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap();
        runtime.block_on(async {
            let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
            let url = format!("ws://{}", listener.local_addr().unwrap());
            let events = Arc::new(AtomicU64::new(0));
            let observed = events.clone();
            let channel = Channel::new(move |_| {
                observed.fetch_add(1, Ordering::SeqCst);
                Ok(())
            });
            let sockets = Sockets::default();
            start(&sockets, "tab-main-a".into(), url, channel);
            let (mut peer, _) = tokio::time::timeout(Duration::from_secs(2), listener.accept())
                .await
                .unwrap()
                .unwrap();
            // Read the HTTP upgrade request, then deliberately never answer it.
            let mut request = Vec::new();
            while !request.ends_with(b"\r\n\r\n") {
                request.push(
                    tokio::time::timeout(Duration::from_secs(2), peer.read_u8())
                        .await
                        .unwrap()
                        .unwrap(),
                );
            }
            sockets.close_view("tab-main-a");
            // Cancellation drops the connecting TCP stream immediately rather
            // than waiting for the 20-second handshake timeout.
            let mut byte = [0];
            assert_eq!(
                tokio::time::timeout(Duration::from_secs(2), peer.read(&mut byte))
                    .await
                    .unwrap()
                    .unwrap(),
                0
            );
            assert_eq!(events.load(Ordering::SeqCst), 0);
        });
    }

    #[test]
    fn destroyed_window_closes_shell_and_tabs_without_touching_other_windows() {
        let sockets = Sockets::default();
        let mut replies = Vec::new();
        for owner in [
            "host-1",
            "tab-host-1-a",
            "tab-host-1-b",
            "host-12",
            "tab-host-12-a",
            "main",
            "tab-main-a",
        ] {
            let (tx, rx) = mpsc::unbounded_channel();
            sockets
                .open
                .lock()
                .unwrap()
                .insert((owner.into(), 1), owned(tx));
            replies.push(rx);
        }
        sockets.close_window("host-1");
        for (i, rx) in replies.iter_mut().enumerate() {
            if i < 3 {
                assert!(matches!(
                    rx.try_recv(),
                    Err(mpsc::error::TryRecvError::Disconnected)
                ));
            } else {
                assert!(matches!(
                    rx.try_recv(),
                    Err(mpsc::error::TryRecvError::Empty)
                ));
            }
        }
    }

    #[test]
    fn a_tab_cannot_send_to_another_tabs_socket_and_closing_releases_only_its_own() {
        let sockets = Sockets::default();
        let (first, mut first_rx) = mpsc::unbounded_channel();
        let (second, mut second_rx) = mpsc::unbounded_channel();
        sockets
            .open
            .lock()
            .unwrap()
            .insert(("a".into(), 1), owned(first));
        sockets
            .open
            .lock()
            .unwrap()
            .insert(("b".into(), 2), owned(second));
        assert!(sender(&sockets, "b", 1, Command::Close(1000, String::new())).is_err());
        sockets.close_view("a");
        assert!(matches!(
            first_rx.try_recv(),
            Err(mpsc::error::TryRecvError::Disconnected)
        ));
        assert!(matches!(
            second_rx.try_recv(),
            Err(mpsc::error::TryRecvError::Empty)
        ));
        assert!(sender(&sockets, "b", 2, Command::Close(1000, String::new())).is_ok());
    }
}
