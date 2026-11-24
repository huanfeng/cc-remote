//! Each server owns a WebView, so its stores, requests and navigation stay independent.
use tauri::{
    webview::WebviewBuilder, LogicalPosition, LogicalSize, Manager, Rect, State, Webview,
    WebviewUrl, Window,
};

#[derive(Default)]
pub struct Tabs(tokio::sync::Mutex<()>);

const TITLE_HEIGHT: f64 = 40.;

pub(crate) fn owns(window: &str, label: &str) -> bool {
    label
        .strip_prefix(&format!("tab-{window}-"))
        .is_some_and(|suffix| {
            !suffix.is_empty()
                && suffix.len() <= 100
                && suffix
                    .bytes()
                    .all(|c| c.is_ascii_alphanumeric() || c == b'-' || c == b'_')
        })
}

fn owned(window: &Window, label: &str) -> Result<Webview, String> {
    if !owns(window.label(), label) {
        return Err("标签页不属于当前窗口".into());
    }
    window
        .webviews()
        .into_iter()
        .find(|view| view.label() == label)
        .ok_or_else(|| "标签页不存在".into())
}

fn bounds(window: &Window) -> Result<LogicalSize<f64>, String> {
    Ok(window
        .inner_size()
        .map_err(|e| e.to_string())?
        .to_logical(window.scale_factor().map_err(|e| e.to_string())?))
}

fn layout(window: &Window) -> Result<(), String> {
    let size = bounds(window)?;
    for view in window.webviews() {
        let is_tab = owns(window.label(), view.label());
        view.set_bounds(Rect {
            position: LogicalPosition::new(0., if is_tab { TITLE_HEIGHT } else { 0. }).into(),
            size: LogicalSize::new(
                size.width,
                (size.height - if is_tab { TITLE_HEIGHT } else { 0. }).max(1.),
            )
            .into(),
        })
        .map_err(|e| e.to_string())?;
    }
    Ok(())
}

fn page(label: &str, host_id: Option<&str>, hash: Option<&str>) -> String {
    let encode = |s: &str| {
        percent_encoding::utf8_percent_encode(s, percent_encoding::NON_ALPHANUMERIC).to_string()
    };
    let mut path = format!("index.html?desktop-tab={}", encode(label));
    if let Some(host) = host_id {
        path.push_str(&format!("&host={}", encode(host)));
    }
    if let Some(hash) = hash {
        if !hash.is_empty() {
            path.push('#');
            path.push_str(hash.trim_start_matches('#'));
        }
    }
    path
}

#[tauri::command]
pub async fn desktop_tab_open(
    window: Window,
    tabs: State<'_, Tabs>,
    label: String,
    host_id: Option<String>,
    hash: Option<String>,
) -> Result<(), String> {
    let _guard = tabs.0.lock().await;
    if !owns(window.label(), &label) {
        return Err("无效的标签页标识".into());
    }
    if window.webviews().iter().any(|view| view.label() == label) {
        return Ok(());
    }
    tauri::async_runtime::spawn_blocking(move || {
        let builder = WebviewBuilder::new(
            &label,
            WebviewUrl::App(page(&label, host_id.as_deref(), hash.as_deref()).into()),
        )
        .focused(false)
        .disable_drag_drop_handler()
        .on_page_load(|view, payload| {
            if matches!(payload.event(), tauri::webview::PageLoadEvent::Started) {
                // Reload replaces the JS context without unmounting React. Its
                // previous requests and terminal sockets must still be released.
                view.state::<crate::net::Net>().close_view(view.label());
                view.state::<crate::ws::Sockets>().close_view(view.label());
            }
        })
        .on_navigation(crate::links::navigation(window.app_handle()))
        .on_new_window(crate::links::popup(window.app_handle()));
        // Create offscreen: WebView2 has no initial visibility option for child views.
        let view = window
            .add_child(
                builder,
                LogicalPosition::new(-100_000., -100_000.),
                LogicalSize::new(1., 1.),
            )
            .map_err(|e| e.to_string())?;
        view.hide().map_err(|e| e.to_string())?;
        view.set_auto_resize(false).map_err(|e| e.to_string())?;
        layout(&window)
    })
    .await
    .map_err(|e| e.to_string())?
}

#[tauri::command]
pub async fn desktop_tab_activate(
    window: Window,
    tabs: State<'_, Tabs>,
    label: Option<String>,
) -> Result<(), String> {
    let _guard = tabs.0.lock().await;
    let target = label
        .as_deref()
        .map(|label| owned(&window, label))
        .transpose()?;
    for view in window
        .webviews()
        .into_iter()
        .filter(|view| owns(window.label(), view.label()))
    {
        if label.as_deref() != Some(view.label()) {
            view.hide().map_err(|e| e.to_string())?;
        }
    }
    layout(&window)?;
    if let Some(view) = target {
        view.show().map_err(|e| e.to_string())?;
        view.set_focus().map_err(|e| e.to_string())?;
    } else if let Some(shell) = window.get_webview(window.label()) {
        shell.set_focus().map_err(|e| e.to_string())?;
    }
    Ok(())
}

#[tauri::command]
pub async fn desktop_tab_close(
    window: Window,
    tabs: State<'_, Tabs>,
    label: String,
) -> Result<(), String> {
    let _guard = tabs.0.lock().await;
    if !owns(window.label(), &label) {
        return Err("标签页不属于当前窗口".into());
    }
    if let Some(view) = window
        .webviews()
        .into_iter()
        .find(|view| view.label() == label)
    {
        view.close().map_err(|e| e.to_string())?;
    }
    window.state::<crate::net::Net>().close_view(&label);
    window.state::<crate::ws::Sockets>().close_view(&label);
    Ok(())
}

#[tauri::command]
pub fn desktop_tab_layout(window: Window) -> Result<(), String> {
    layout(&window)
}

pub fn window_event(window: &Window, event: &tauri::WindowEvent) {
    if matches!(
        event,
        tauri::WindowEvent::Resized(_) | tauri::WindowEvent::ScaleFactorChanged { .. }
    ) {
        let _ = layout(window);
    }
    if matches!(event, tauri::WindowEvent::Destroyed) {
        // Tauri removes its WebView registry before delivering Destroyed.
        // Transport ownership survives there, so do not enumerate window.webviews().
        window
            .state::<crate::net::Net>()
            .close_window(window.label());
        window
            .state::<crate::ws::Sockets>()
            .close_window(window.label());
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn labels_cannot_target_shell_or_another_window() {
        assert!(owns("main", "tab-main-abc_123"));
        for label in [
            "main",
            "tab-host-1",
            "tab-main-",
            "tab-main-../main",
            "tab-main-x?host=bad",
        ] {
            assert!(!owns("main", label));
        }
        assert!(!owns("host-1", "tab-main-abc"));
    }
    #[test]
    fn page_encodes_host_query_and_keeps_session_fragment() {
        assert_eq!(
            page("tab-main-1", Some("a&host=b"), Some("#/session/abc")),
            "index.html?desktop-tab=tab%2Dmain%2D1&host=a%26host%3Db#/session/abc"
        );
        assert_eq!(
            page("tab-main-1", None, None),
            "index.html?desktop-tab=tab%2Dmain%2D1"
        );
    }
}
