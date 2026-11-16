//! Desktop-owned tray. Only explicit refreshes call the provider refresh route.
//! Native hover uses windui; only the main client needs a WebView.
mod hover;
mod model;
#[cfg(windows)]
mod panel;
mod placement;

use model::Snapshot;
use serde::Serialize;
use std::{path::PathBuf, sync::Mutex, time::Duration};
use tauri::{
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    Emitter, Manager,
};
use tokio::sync::mpsc;

#[derive(Clone, Default, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct View {
    #[serde(flatten)]
    pub snapshot: Snapshot,
    pub favorites: Vec<String>,
    pub connected: bool,
    pub loading: bool,
    pub busy: bool,
    pub error: Option<String>,
    pub feedback: Option<String>,
    pub feedback_error: bool,
    pub feedback_id: u64,
}

pub struct UsageTray {
    view: Mutex<View>,
    refresh: mpsc::Sender<()>,
    preferences: PathBuf,
    hover: hover::Handle,
    windows: std::sync::mpsc::Sender<WindowAction>,
}

enum WindowAction {
    ShowMain,
    History(Option<String>),
    About { check_updates: bool },
}

fn window_action(app: &tauri::AppHandle, action: WindowAction) {
    let _ = app.state::<UsageTray>().windows.send(action);
}

fn favorites(value: &serde_json::Value) -> Vec<String> {
    let ids = value["favoriteAccounts"]
        .as_array()
        .map(|a| a.iter().filter_map(|v| v.as_str()).collect::<Vec<_>>())
        .unwrap_or_else(|| value["focusedAccount"].as_str().into_iter().collect());
    let mut result = Vec::new();
    for id in ids {
        if !id.is_empty() && !result.iter().any(|v| v == id) {
            result.push(id.to_string());
        }
    }
    result
}

fn read_preferences(path: &std::path::Path) -> Option<serde_json::Value> {
    serde_json::from_slice(&std::fs::read(path).ok()?).ok()
}

fn publish(app: &tauri::AppHandle) {
    let state = app.state::<UsageTray>();
    let view = state.view.lock().unwrap().clone();
    state.hover.update(&view);
    #[cfg(not(windows))]
    if let Some(tray) = app.tray_by_id("cc-remote") {
        let count = view.favorites.len();
        let _ = tray.set_tooltip(Some(format!(
            "CC Remote · {count} 个收藏账号 · 点击打开客户端"
        )));
    }
}

fn request_refresh(app: &tauri::AppHandle) -> Result<(), String> {
    let state = app.state::<UsageTray>();
    {
        let mut view = state.view.lock().unwrap();
        if view.busy || view.loading {
            return Ok(());
        }
        state
            .refresh
            .try_send(())
            .map_err(|_| "刷新队列暂时不可用")?;
        view.busy = true;
        view.feedback = None;
    }
    publish(app);
    Ok(())
}

pub fn show_main(app: &tauri::AppHandle) {
    app.state::<UsageTray>().hover.suppress(true);
    window_action(app, WindowAction::ShowMain);
}

fn show_main_now(app: &tauri::AppHandle) -> Result<(), String> {
    let window = if let Some(window) = app.get_window("main") {
        window
    } else {
        let config = app
            .config()
            .app
            .windows
            .iter()
            .find(|window| window.label == "main")
            .ok_or("主窗口配置不存在")?;
        tauri::WebviewWindowBuilder::from_config(app, config)
            .map_err(|e| e.to_string())?
            .title(format!("CC Remote v{}", app.package_info().version))
            .build()
            .map_err(|e| e.to_string())?
            .as_ref()
            .window()
    };
    window.unminimize().map_err(|e| e.to_string())?;
    window.show().map_err(|e| e.to_string())?;
    window.set_focus().map_err(|e| e.to_string())
}

fn open_history(app: &tauri::AppHandle, account: Option<String>) {
    app.state::<UsageTray>().hover.suppress(true);
    window_action(app, WindowAction::History(account));
}

fn open_history_now(app: &tauri::AppHandle, account: Option<String>) -> Result<(), String> {
    *app.state::<crate::history::Requests>().0.lock().unwrap() =
        Some(crate::history::HistoryRequest {
            account,
            ..Default::default()
        });
    show_main_now(app)?;
    let _ = app.emit_to("main", "usage-history-requested", ());
    app.state::<UsageTray>().hover.suppress(true);
    Ok(())
}

fn open_about(app: &tauri::AppHandle, check_updates: bool) {
    app.state::<UsageTray>().hover.suppress(true);
    window_action(app, WindowAction::About { check_updates });
}

fn open_about_now(app: &tauri::AppHandle, check_updates: bool) -> Result<(), String> {
    app.state::<crate::about::Requests>().request();
    show_main_now(app)?;
    let _ = app.emit_to("main", "desktop-about-requested", ());
    if check_updates {
        let app = app.clone();
        tauri::async_runtime::spawn(async move {
            crate::update::desktop_update_check(app).await;
        });
    }
    Ok(())
}

pub fn setup(app: &mut tauri::App) -> Result<(), Box<dyn std::error::Error>> {
    let preferences = app.path().app_config_dir()?.join("tray-preferences.json");
    let previous = std::env::var_os("LOCALAPPDATA")
        .map(PathBuf::from)
        .map(|p| p.join("cc-remote-tray/preferences.json"));
    let saved = read_preferences(&preferences)
        .or_else(|| previous.and_then(|p| read_preferences(&p)))
        .unwrap_or_default();
    let (tx, rx) = mpsc::channel(1);
    let (windows, window_requests) = std::sync::mpsc::channel();
    app.manage(UsageTray {
        view: Mutex::new(View {
            favorites: favorites(&saved),
            loading: true,
            ..Default::default()
        }),
        refresh: tx,
        preferences,
        hover: hover::Handle::start(app.handle().clone()),
        windows,
    });
    let menu = tauri::menu::Menu::with_items(
        app,
        &[
            &tauri::menu::MenuItem::with_id(app, "open", "打开 CC Remote", true, None::<&str>)?,
            &tauri::menu::MenuItem::with_id(app, "refresh", "刷新用量", true, None::<&str>)?,
            &tauri::menu::MenuItem::with_id(app, "history", "全部用量历史", true, None::<&str>)?,
            &tauri::menu::MenuItem::with_id(app, "update", "检查桌面端更新", true, None::<&str>)?,
            &tauri::menu::MenuItem::with_id(app, "about", "关于与更新", true, None::<&str>)?,
            &tauri::menu::PredefinedMenuItem::separator(app)?,
            &tauri::menu::MenuItem::with_id(
                app,
                "quit",
                "退出 CC Remote（后台继续记录）",
                true,
                None::<&str>,
            )?,
        ],
    )?;
    let mut tray = TrayIconBuilder::with_id("cc-remote")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_menu_event(|app, event| match event.id.as_ref() {
            "open" => show_main(app),
            "refresh" => {
                let _ = request_refresh(app);
            }
            "history" => open_history(app, None),
            "update" => open_about(app, true),
            "about" => open_about(app, false),
            "quit" => app.exit(0),
            _ => {}
        })
        .on_tray_icon_event(|tray, event| {
            let app = tray.app_handle();
            match event {
                TrayIconEvent::Click {
                    button: MouseButton::Left,
                    button_state: MouseButtonState::Up,
                    ..
                } => {
                    show_main(app);
                }
                TrayIconEvent::Click {
                    button: MouseButton::Right,
                    ..
                } => {
                    app.state::<UsageTray>().hover.suppress(true);
                }
                TrayIconEvent::Enter { rect, .. } | TrayIconEvent::Move { rect, .. } => {
                    let position = rect.position.to_physical::<i32>(1.0);
                    let size = rect.size.to_physical::<i32>(1.0);
                    app.state::<UsageTray>().hover.enter(
                        position.x,
                        position.y,
                        size.width,
                        size.height,
                    );
                }
                TrayIconEvent::Leave { .. } => app.state::<UsageTray>().hover.leave(),
                _ => {}
            }
        });
    if let Some(icon) = app.default_window_icon() {
        tray = tray.icon(icon.clone());
    }
    #[cfg(not(windows))]
    {
        tray = tray.tooltip("CC Remote · 订阅用量");
    }
    tray.build(app)?;
    let handle = app.handle().clone();
    // WebView2 creation (and window activation) must not block a Windows
    // event/IPC callback. One worker also serializes rapid opens,
    // so two requests cannot concurrently create the same window label.
    std::thread::Builder::new()
        .name("desktop-windows".into())
        .spawn(move || {
            for action in window_requests {
                let result = match action {
                    WindowAction::ShowMain => show_main_now(&handle),
                    WindowAction::History(account) => open_history_now(&handle, account),
                    WindowAction::About { check_updates } => open_about_now(&handle, check_updates),
                };
                if let Err(error) = result {
                    handle.state::<UsageTray>().hover.suppress(false);
                    eprintln!("CC Remote window action failed: {error}");
                }
            }
        })?;
    publish(app.handle());
    let handle = app.handle().clone();
    tauri::async_runtime::spawn(async move {
        worker(handle, rx).await;
    });
    Ok(())
}

async fn read_snapshot(
    client: &reqwest::Client,
    connection: &(String, PathBuf),
    refresh: bool,
) -> Result<Snapshot, String> {
    let token = std::fs::read_to_string(connection.1.join("access-token"))
        .map_err(|_| "无法读取本机后台访问令牌")?;
    if token.trim().is_empty() {
        return Err("本机后台访问令牌为空".into());
    }
    let url = format!(
        "{}/api/usage/monitor{}",
        connection.0,
        if refresh { "/refresh" } else { "" }
    );
    let request = if refresh {
        client.post(url)
    } else {
        client.get(url)
    };
    let response = request
        .bearer_auth(token.trim())
        .send()
        .await
        .map_err(|_| "无法连接本机后台，请检查服务是否运行")?;
    if !response.status().is_success() {
        return Err(format!(
            "读取用量失败（HTTP {}）",
            response.status().as_u16()
        ));
    }
    if response
        .content_length()
        .is_some_and(|n| n > 4 * 1024 * 1024)
    {
        return Err("用量响应过大".into());
    }
    let mut response = response;
    let mut bytes = Vec::new();
    while let Some(chunk) = response.chunk().await.map_err(|_| "无法读取用量响应")? {
        if bytes.len() + chunk.len() > 4 * 1024 * 1024 {
            return Err("用量响应过大".into());
        }
        bytes.extend_from_slice(&chunk);
    }
    serde_json::from_slice(&bytes).map_err(|_| "后台不支持订阅用量监控，请更新后台".into())
}

fn apply_result(view: &mut View, result: Result<Snapshot, String>, manual: bool) {
    view.loading = false;
    if manual {
        view.busy = false;
        view.feedback_id += 1;
    }
    match result {
        Ok(snapshot) => {
            view.snapshot = snapshot;
            view.connected = true;
            view.error = None;
            if manual {
                view.feedback = Some("已更新用量读数（遵循后台缓存与冷却）".into());
                view.feedback_error = false;
            }
        }
        Err(error) => {
            view.connected = false;
            view.error = Some(error.clone());
            if manual {
                view.feedback = Some(error);
                view.feedback_error = true;
            }
        }
    }
}

async fn worker(app: tauri::AppHandle, mut rx: mpsc::Receiver<()>) {
    let client = reqwest::Client::builder()
        .no_proxy()
        .redirect(reqwest::redirect::Policy::none())
        .timeout(Duration::from_secs(15))
        .build()
        .expect("local usage HTTP client");
    let mut connection = None;
    let mut manual = false;
    loop {
        let result = async {
            if connection.is_none() {
                connection = Some(crate::service::usage_monitor_location().await?);
            }
            read_snapshot(&client, connection.as_ref().unwrap(), manual).await
        }
        .await;
        if result.is_err() {
            connection = None;
        }
        {
            let state = app.state::<UsageTray>();
            apply_result(&mut state.view.lock().unwrap(), result, manual);
        }
        publish(&app);
        manual = match tokio::time::timeout(Duration::from_secs(30), rx.recv()).await {
            Ok(Some(())) => true,
            Ok(None) => return,
            Err(_) => false,
        };
    }
}

fn set_favorite(app: tauri::AppHandle, id: String, favorite: bool) -> Result<View, String> {
    let state = app.state::<UsageTray>();
    {
        let mut view = state.view.lock().unwrap();
        if id.is_empty() {
            return Err("账号 ID 为空".into());
        }
        let mut favorites = view.favorites.clone();
        favorites.retain(|v| v != &id);
        if favorite {
            favorites.push(id);
        }
        let parent = state.preferences.parent().ok_or("无法确定收藏目录")?;
        std::fs::create_dir_all(parent).map_err(|_| "无法创建收藏目录")?;
        let pending = state.preferences.with_extension("json.tmp");
        std::fs::write(
            &pending,
            serde_json::json!({ "favoriteAccounts": favorites }).to_string(),
        )
        .and_then(|_| std::fs::rename(pending, &state.preferences))
        .map_err(|_| "无法保存收藏")?;
        view.favorites = favorites;
    }
    publish(&app);
    let view = state.view.lock().unwrap().clone();
    Ok(view)
}

pub fn window_event(window: &tauri::Window, event: &tauri::WindowEvent) {
    if let tauri::WindowEvent::CloseRequested { api, .. } = event {
        if window.label() == "main" {
            api.prevent_close();
            let _ = window.hide();
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn cache_reads_never_refresh_and_each_request_rereads_the_token() {
        use std::io::{Read, Write};
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let origin = format!("http://{}", listener.local_addr().unwrap());
        let server = std::thread::spawn(move || {
            let mut requests = Vec::new();
            for _ in 0..2 {
                let (mut stream, _) = listener.accept().unwrap();
                stream
                    .set_read_timeout(Some(Duration::from_secs(3)))
                    .unwrap();
                let mut bytes = Vec::new();
                let mut byte = [0];
                while !bytes.ends_with(b"\r\n\r\n") {
                    stream.read_exact(&mut byte).unwrap();
                    bytes.push(byte[0]);
                }
                requests.push(String::from_utf8(bytes).unwrap());
                let body = r#"{"intervalMinutes":10,"accounts":[]}"#;
                write!(
                    stream,
                    "HTTP/1.1 200 OK\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                    body.len(),
                    body
                )
                .unwrap();
            }
            requests
        });
        let directory =
            std::env::temp_dir().join(format!("cc-desktop-tray-test-{}", std::process::id()));
        std::fs::create_dir_all(&directory).unwrap();
        let connection = (origin, directory.clone());
        let runtime = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap();
        runtime.block_on(async {
            let client = reqwest::Client::builder()
                .no_proxy()
                .timeout(Duration::from_secs(3))
                .build()
                .unwrap();
            std::fs::write(directory.join("access-token"), "first-token").unwrap();
            read_snapshot(&client, &connection, false).await.unwrap();
            std::fs::write(directory.join("access-token"), "rotated-token\n").unwrap();
            read_snapshot(&client, &connection, true).await.unwrap();
        });
        let requests = server.join().unwrap();
        assert!(requests[0].starts_with("GET /api/usage/monitor HTTP/1.1"));
        assert!(requests[1].starts_with("POST /api/usage/monitor/refresh HTTP/1.1"));
        assert!(requests[0]
            .to_lowercase()
            .contains("authorization: bearer first-token"));
        assert!(requests[1]
            .to_lowercase()
            .contains("authorization: bearer rotated-token"));
        std::fs::remove_file(directory.join("access-token")).unwrap();
        std::fs::remove_dir(directory).unwrap();
    }

    #[test]
    fn favorites_migrate_without_dropping_explicit_empty_or_missing_accounts() {
        assert_eq!(
            favorites(&serde_json::json!({ "focusedAccount": "old" })),
            vec!["old"]
        );
        assert_eq!(
            favorites(&serde_json::json!({ "favoriteAccounts": ["a", "b", "a", "", null] })),
            vec!["a", "b"]
        );
        assert!(
            favorites(&serde_json::json!({ "focusedAccount": "old", "favoriteAccounts": [] }))
                .is_empty()
        );
    }
    #[test]
    fn offline_retains_readings_and_only_manual_results_generate_feedback() {
        let mut view = View {
            favorites: vec!["old".into()],
            snapshot: Snapshot {
                accounts: vec![model::Account {
                    id: "old".into(),
                    ..Default::default()
                }],
                ..Default::default()
            },
            ..Default::default()
        };
        apply_result(&mut view, Err("offline".into()), false);
        assert_eq!(view.snapshot.accounts.len(), 1);
        assert_eq!(view.favorites, vec!["old"]);
        assert!(view.feedback.is_none());
        view.busy = true;
        apply_result(&mut view, Err("refresh failed".into()), true);
        assert!(!view.busy);
        assert!(view.feedback_error);
        assert_eq!(view.feedback_id, 1);
        apply_result(&mut view, Ok(Snapshot::default()), false);
        assert_eq!(view.feedback_id, 1);
        assert!(view.connected);
        assert_eq!(view.favorites, vec!["old"]);
    }
}
