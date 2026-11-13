// A tray launch carries routing information, never credentials. Keep a pending
// request until the main WebView has installed its event listener.
use std::sync::Mutex;

use serde::{Deserialize, Serialize};
use tauri::{Manager, State, WebviewWindow};

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct HistoryRequest {
    pub host: Option<String>,
    pub data_dir: Option<String>,
    pub account: Option<String>,
}

impl HistoryRequest {
    pub fn from_args(args: impl IntoIterator<Item = String>) -> Option<Self> {
        let mut args = args.into_iter();
        let mut request = Self::default();
        let mut enabled = false;
        while let Some(arg) = args.next() {
            match arg.as_str() {
                "--usage-history" => enabled = true,
                "--host" => request.host = args.next(),
                "--data-dir" => request.data_dir = args.next(),
                "--account" => request.account = args.next(),
                _ => {}
            }
        }
        enabled.then_some(request)
    }
}

pub struct Requests(pub Mutex<Option<HistoryRequest>>);

pub fn receive(app: &tauri::AppHandle, args: Vec<String>) {
    if let Some(request) = HistoryRequest::from_args(args) {
        *app.state::<Requests>().0.lock().unwrap() = Some(request);
        if let Some(window) = app.get_webview_window("main") {
            use tauri::Emitter;
            let _ = window.emit("usage-history-requested", ());
        }
    }
}

#[tauri::command]
pub fn take_usage_history_request(
    window: WebviewWindow,
    requests: State<'_, Requests>,
) -> Option<HistoryRequest> {
    if window.label() != "main" {
        return None;
    }
    requests.0.lock().unwrap().take()
}

// Credentials read from disk may only be paired with an explicit local origin.
pub fn local_origin(value: &str) -> Result<String, String> {
    let url = reqwest::Url::parse(value).map_err(|_| "Invalid local host address")?;
    let hostname = url.host_str().unwrap_or("").trim_matches(['[', ']']);
    let local = hostname.eq_ignore_ascii_case("localhost")
        || hostname
            .parse::<std::net::IpAddr>()
            .is_ok_and(|ip| ip.is_loopback());
    if !local
        || !matches!(url.scheme(), "http" | "https")
        || !url.username().is_empty()
        || url.password().is_some()
        || url.query().is_some()
        || url.fragment().is_some()
        || url.path() != "/"
    {
        return Err("Usage history requires a local HTTP(S) host address".into());
    }
    Ok(url.as_str().trim_end_matches('/').to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn ordinary_launch_has_no_history_request() {
        assert!(HistoryRequest::from_args([
            "app".into(),
            "--host".into(),
            "http://localhost".into()
        ])
        .is_none());
    }

    #[test]
    fn tray_launch_keeps_routing_without_a_token() {
        let args = [
            "app",
            "--usage-history",
            "--host",
            "http://127.0.0.1:8790",
            "--data-dir",
            "C:/my data",
            "--account",
            "claude:123",
        ];
        let request = HistoryRequest::from_args(args.map(str::to_string)).unwrap();
        assert_eq!(request.host.as_deref(), Some("http://127.0.0.1:8790"));
        assert_eq!(request.data_dir.as_deref(), Some("C:/my data"));
        assert_eq!(request.account.as_deref(), Some("claude:123"));
    }

    #[test]
    fn rejects_remote_or_ambiguous_token_destinations() {
        for host in [
            "https://example.com",
            "http://localhost.example.com",
            "file:///tmp",
            "http://user@localhost",
            "http://localhost/?redirect=x",
            "http://localhost/path",
            "http://192.168.1.2",
        ] {
            assert!(local_origin(host).is_err(), "{host}");
        }
        assert_eq!(
            local_origin("http://127.0.0.1:8790/").unwrap(),
            "http://127.0.0.1:8790"
        );
        assert!(local_origin("http://[::1]:8790").is_ok());
    }
}
