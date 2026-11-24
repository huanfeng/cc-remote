// The CC Remote desktop app: the web frontend (web/, `vite build --mode
// desktop`) in a native window. It holds several hosts like the standalone page,
// talks to them from Rust (net.rs, ws.rs) and manages the cc-remote service on
// this computer (service.rs).
mod about;
mod core_update;
mod history;
mod links;
mod net;
mod paths;
mod service;
mod tabs;
mod update;
mod usage_tray;
mod ws;

pub fn run() {
    tauri::Builder::default()
        .manage(about::Requests::default())
        .manage(core_update::CoreUpdate::default())
        .manage(update::ComponentInstallLock::default())
        .manage(tabs::Tabs::default())
        .manage(history::Requests(std::sync::Mutex::new(
            history::HistoryRequest::from_args(std::env::args()),
        )))
        // A second launch brings the open window forward instead.
        .plugin(tauri_plugin_single_instance::init(|app, args, _cwd| {
            let tray_only = args.iter().any(|arg| arg == "--tray");
            history::receive(app, args);
            if !tray_only {
                usage_tray::show_main(app);
            }
        }))
        .plugin(
            tauri_plugin_window_state::Builder::default()
                // Window decorations follow current config, never old saved state.
                .with_state_flags(
                    tauri_plugin_window_state::StateFlags::all()
                        & !tauri_plugin_window_state::StateFlags::DECORATIONS,
                )
                .build(),
        )
        .plugin(tauri_plugin_notification::init())
        .plugin(
            tauri_plugin_opener::Builder::new()
                .open_js_links_on_click(false)
                .build(),
        )
        .plugin(tauri_plugin_updater::Builder::new().build())
        .manage(net::Net::new())
        .manage(ws::Sockets::default())
        .setup(|app| {
            usage_tray::setup(app)?;
            update::setup(app.handle());
            if !std::env::args().any(|arg| arg == "--tray") {
                usage_tray::show_main(app.handle());
            }
            Ok(())
        })
        .on_window_event(|window, event| {
            usage_tray::window_event(window, event);
            tabs::window_event(window, event);
        })
        .invoke_handler(tauri::generate_handler![
            links::desktop_window_open,
            tabs::desktop_tab_open,
            tabs::desktop_tab_activate,
            tabs::desktop_tab_close,
            tabs::desktop_tab_layout,
            paths::open_local_path,
            net::http_request,
            net::http_abort,
            ws::ws_open,
            ws::ws_send,
            ws::ws_send_text,
            ws::ws_close,
            service::local_service_status,
            service::local_service_token,
            service::local_service_action,
            history::take_usage_history_request,
            about::take_desktop_about_request,
            service::usage_history_connection,
            update::desktop_update_status,
            update::desktop_update_check,
            update::desktop_update_install,
            core_update::core_update_status,
            core_update::core_update_check,
            core_update::core_update_install,
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
            .invoke_handler(tauri::generate_handler![
                crate::net::http_request,
                crate::net::http_abort
            ])
            .build(mock_context(noop_assets()))
            .expect("mock app")
    }

    fn request(
        app: &tauri::App<tauri::test::MockRuntime>,
        meta: serde_json::Value,
        body: Vec<u8>,
    ) -> Result<(serde_json::Value, Vec<u8>), serde_json::Value> {
        let label = format!("w{}", meta["id"]);
        let webview = WebviewWindowBuilder::new(app, &label, Default::default())
            .build()
            .unwrap();
        let mut headers = tauri::http::HeaderMap::new();
        let encoded = percent_encoding::utf8_percent_encode(
            &meta.to_string(),
            percent_encoding::NON_ALPHANUMERIC,
        )
        .to_string();
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
        let InvokeResponseBody::Raw(bytes) = reply else {
            panic!("expected a raw reply")
        };
        let len = u32::from_be_bytes(bytes[..4].try_into().unwrap()) as usize;
        let head = serde_json::from_slice(&bytes[4..4 + len]).unwrap();
        Ok((head, bytes[4 + len..].to_vec()))
    }

    #[test]
    fn refuses_other_schemes() {
        let app = app();
        let err = request(
            &app,
            serde_json::json!({ "id": 1, "method": "GET", "url": "file:///etc/passwd" }),
            vec![],
        )
        .unwrap_err();
        assert!(err.to_string().contains("unsupported"), "{err}");
    }

    #[test]
    fn unreachable_host_is_an_error() {
        let app = app();
        let err = request(
            &app,
            serde_json::json!({ "id": 2, "method": "GET", "url": "http://127.0.0.1:1/api/info" }),
            vec![],
        )
        .unwrap_err();
        assert!(!err.to_string().is_empty());
    }

    /// A host refuses a browser Origin it does not know; the request from Rust has none.
    #[test]
    fn live_host_answers_without_origin() {
        let (Ok(host), Ok(token)) = (
            std::env::var("CC_TEST_HOST"),
            std::env::var("CC_TEST_TOKEN"),
        ) else {
            return;
        };
        let app = app();
        let meta = serde_json::json!({
            "id": 3, "method": "GET", "url": format!("{host}/api/info"),
            "headers": [["Authorization", format!("Bearer {token}")], ["Origin", "http://tauri.localhost"]],
        });
        let (head, body) = request(&app, meta, vec![]).unwrap();
        assert_eq!(head["status"], 200, "{head}");
        let info: serde_json::Value = serde_json::from_slice(&body).unwrap();
        assert!(
            info.get("version").is_some() || info.get("name").is_some(),
            "{info}"
        );
        // An ETag revalidation comes back as 304 with no body.
        let etag = head["headers"]
            .as_array()
            .unwrap()
            .iter()
            .find(|h| h[0] == "etag")
            .map(|h| h[1].as_str().unwrap().to_string());
        if let Some(tag) = etag {
            let meta = serde_json::json!({ "id": 4, "method": "GET", "url": format!("{host}/api/info"), "headers": [["Authorization", format!("Bearer {token}")], ["If-None-Match", tag]] });
            let (head, body) = request(&app, meta, vec![]).unwrap();
            assert_eq!(head["status"], 304);
            assert!(body.is_empty());
        }
    }
}
