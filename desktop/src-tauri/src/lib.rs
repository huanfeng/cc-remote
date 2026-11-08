// The CC Remote desktop app: the web frontend (web/, `vite build --mode
// desktop`) in a native window. It holds several hosts like the standalone page,
// talks to them from Rust (net.rs, ws.rs) and manages the cc-remote service on
// this computer (service.rs).
mod net;
mod service;
mod ws;

use tauri::Manager;

pub fn run() {
    tauri::Builder::default()
        // A second launch brings the open window forward instead.
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.unminimize();
                let _ = window.show();
                let _ = window.set_focus();
            }
        }))
        .plugin(tauri_plugin_window_state::Builder::default().build())
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_opener::init())
        .manage(net::Net::new())
        .manage(ws::Sockets::default())
        .invoke_handler(tauri::generate_handler![
            net::http_request,
            net::http_abort,
            ws::ws_open,
            ws::ws_send,
            ws::ws_send_text,
            ws::ws_close,
            service::local_service_status,
            service::local_service_token,
            service::local_service_action,
        ])
        .run(tauri::generate_context!())
        .expect("error while running the CC Remote desktop app");
}

// Through the real IPC path (mock runtime). The live checks need a host:
// CC_TEST_HOST=http://127.0.0.1:8790 CC_TEST_TOKEN=... cargo test
#[cfg(test)]
mod tests {
    use tauri::ipc::{CallbackFn, InvokeBody, InvokeResponseBody};
    use tauri::test::{get_ipc_response, mock_builder, mock_context, noop_assets, INVOKE_KEY};
    use tauri::webview::InvokeRequest;
    use tauri::WebviewWindowBuilder;

    fn app() -> tauri::App<tauri::test::MockRuntime> {
        mock_builder()
            .manage(crate::net::Net::new())
            .invoke_handler(tauri::generate_handler![crate::net::http_request, crate::net::http_abort])
            .build(mock_context(noop_assets()))
            .expect("mock app")
    }

    fn request(app: &tauri::App<tauri::test::MockRuntime>, meta: serde_json::Value, body: Vec<u8>) -> Result<(serde_json::Value, Vec<u8>), serde_json::Value> {
        let label = format!("w{}", meta["id"]);
        let webview = WebviewWindowBuilder::new(app, &label, Default::default()).build().unwrap();
        let mut headers = tauri::http::HeaderMap::new();
        let encoded = percent_encoding::utf8_percent_encode(&meta.to_string(), percent_encoding::NON_ALPHANUMERIC).to_string();
        headers.insert("x-cc-meta", encoded.parse().unwrap());
        let reply = get_ipc_response(
            &webview,
            InvokeRequest {
                cmd: "http_request".into(),
                callback: CallbackFn(0),
                error: CallbackFn(1),
                url: "http://tauri.localhost".parse().unwrap(),
                body: InvokeBody::Raw(body),
                headers,
                invoke_key: INVOKE_KEY.to_string(),
            },
        )?;
        let InvokeResponseBody::Raw(bytes) = reply else { panic!("expected a raw reply") };
        let len = u32::from_be_bytes(bytes[..4].try_into().unwrap()) as usize;
        let head = serde_json::from_slice(&bytes[4..4 + len]).unwrap();
        Ok((head, bytes[4 + len..].to_vec()))
    }

    #[test]
    fn refuses_other_schemes() {
        let app = app();
        let err = request(&app, serde_json::json!({ "id": 1, "method": "GET", "url": "file:///etc/passwd" }), vec![]).unwrap_err();
        assert!(err.to_string().contains("unsupported"), "{err}");
    }

    #[test]
    fn unreachable_host_is_an_error() {
        let app = app();
        let err = request(&app, serde_json::json!({ "id": 2, "method": "GET", "url": "http://127.0.0.1:1/api/info" }), vec![]).unwrap_err();
        assert!(!err.to_string().is_empty());
    }

    /// A host refuses a browser Origin it does not know; the request from Rust has none.
    #[test]
    fn live_host_answers_without_origin() {
        let (Ok(host), Ok(token)) = (std::env::var("CC_TEST_HOST"), std::env::var("CC_TEST_TOKEN")) else { return };
        let app = app();
        let meta = serde_json::json!({
            "id": 3, "method": "GET", "url": format!("{host}/api/info"),
            "headers": [["Authorization", format!("Bearer {token}")], ["Origin", "http://tauri.localhost"]],
        });
        let (head, body) = request(&app, meta, vec![]).unwrap();
        assert_eq!(head["status"], 200, "{head}");
        let info: serde_json::Value = serde_json::from_slice(&body).unwrap();
        assert!(info.get("version").is_some() || info.get("name").is_some(), "{info}");
        // An ETag revalidation comes back as 304 with no body.
        let etag = head["headers"].as_array().unwrap().iter().find(|h| h[0] == "etag").map(|h| h[1].as_str().unwrap().to_string());
        if let Some(tag) = etag {
            let meta = serde_json::json!({ "id": 4, "method": "GET", "url": format!("{host}/api/info"), "headers": [["Authorization", format!("Bearer {token}")], ["If-None-Match", tag]] });
            let (head, body) = request(&app, meta, vec![]).unwrap();
            assert_eq!(head["status"], 304);
            assert!(body.is_empty());
        }
    }
}
