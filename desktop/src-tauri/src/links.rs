//! Route links for shell and child WebViews, including native popup requests.
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};
use tauri::{
    utils::config::FrontendDist,
    webview::{NewWindowFeatures, NewWindowResponse},
    AppHandle, Url, WebviewUrl, WebviewWindowBuilder, Wry,
};

fn app_url(app: &AppHandle) -> Url {
    let build = &app.config().build;
    if tauri::is_dev() {
        if let Some(url) = &build.dev_url {
            return url.clone();
        }
    } else if let Some(FrontendDist::Url(url)) = &build.frontend_dist {
        return url.clone();
    }
    // Mirrors Tauri's bundled asset URL. Our WebViews use its default HTTP scheme
    // on Windows, and the tauri custom protocol on macOS and Linux.
    let bundled = if cfg!(windows) {
        "http://tauri.localhost"
    } else {
        "tauri://localhost"
    };
    Url::parse(bundled).expect("valid bundled app URL")
}

#[derive(Debug, PartialEq, Eq)]
enum Destination {
    App,
    External,
    Unsupported,
}

fn destination(url: &Url, app: &Url) -> Destination {
    if url.scheme() == app.scheme()
        && url.host_str() == app.host_str()
        && url.port() == app.port()
        && url.username().is_empty()
        && url.password().is_none()
    {
        Destination::App
    } else if matches!(url.scheme(), "http" | "https" | "mailto") {
        Destination::External
    } else {
        Destination::Unsupported
    }
}

fn open_external(url: &Url) {
    if let Err(err) = tauri_plugin_opener::open_url(url.as_str(), None::<&str>) {
        eprintln!("Could not open an external link: {err}");
    }
}

pub(crate) fn navigation(app: &AppHandle) -> impl Fn(&Url) -> bool + Send + 'static {
    let local = app_url(app);
    move |url| match destination(url, &local) {
        Destination::App => true,
        Destination::External => {
            open_external(url);
            false
        }
        Destination::Unsupported => false,
    }
}

pub(crate) fn popup(
    app: &AppHandle,
) -> impl Fn(Url, NewWindowFeatures) -> NewWindowResponse<Wry> + Send + 'static {
    let local = app_url(app);
    move |url, _| {
        if destination(&url, &local) == Destination::External {
            open_external(&url);
        }
        // An auxiliary browser request should never create a WebView. The
        // app's host switcher and new-window action use the validated command.
        NewWindowResponse::Deny
    }
}

fn app_page(mut url: Url, app: &Url) -> Option<String> {
    if destination(&url, app) != Destination::App || !matches!(url.path(), "" | "/" | "/index.html")
    {
        return None;
    }
    // A host link made inside a tab can inherit its desktop-tab query.
    // Secondary windows own their workspace instead of acting as child tabs.
    let query: Vec<_> = url
        .query_pairs()
        .filter(|(key, _)| key != "desktop-tab")
        .map(|(key, val)| (key.into_owned(), val.into_owned()))
        .collect();
    url.set_query(None);
    let mut page = "index.html".to_owned();
    if !query.is_empty() {
        url.query_pairs_mut().extend_pairs(query);
        page.push('?');
        page.push_str(url.query().expect("serialized query"));
    }
    if let Some(fragment) = url.fragment() {
        page.push('#');
        page.push_str(fragment);
    }
    Some(page)
}

#[tauri::command]
pub async fn desktop_window_open(app: AppHandle, url: String) -> Result<(), String> {
    let page = Url::parse(&url)
        .ok()
        .and_then(|url| app_page(url, &app_url(&app)))
        .ok_or("无效的应用窗口地址")?;
    tauri::async_runtime::spawn_blocking(move || {
        static NEXT_WINDOW: AtomicU64 = AtomicU64::new(1);
        let stamp = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_millis();
        let label = format!(
            "host-{stamp}-{}",
            NEXT_WINDOW.fetch_add(1, Ordering::Relaxed)
        );
        let builder = WebviewWindowBuilder::new(&app, label, WebviewUrl::App(page.into()))
            .title(format!("CC Remote v{}", app.package_info().version))
            .inner_size(1200., 800.)
            .min_inner_size(720., 480.)
            .decorations(cfg!(target_os = "macos"))
            .disable_drag_drop_handler()
            .on_navigation(navigation(&app))
            .on_new_window(popup(&app));
        #[cfg(target_os = "macos")]
        let builder = builder
            .title_bar_style(tauri::TitleBarStyle::Overlay)
            .hidden_title(true);
        builder.build().map(|_| ()).map_err(|e| e.to_string())
    })
    .await
    .map_err(|e| e.to_string())?
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn navigation_routes_only_supported_external_schemes() {
        let app = Url::parse("tauri://localhost/").unwrap();
        for (href, expected) in [
            ("tauri://localhost/index.html?host=a", Destination::App),
            ("tauri://other/index.html", Destination::Unsupported),
            ("tauri://user@localhost/", Destination::Unsupported),
            ("https://example.com/link", Destination::External),
            ("http://127.0.0.1:8080/", Destination::External),
            ("mailto:name@example.com", Destination::External),
            ("about:blank", Destination::Unsupported),
            ("blob:tauri://localhost/file", Destination::Unsupported),
            ("file:///C:/private.txt", Destination::Unsupported),
            ("example:run", Destination::Unsupported),
        ] {
            assert_eq!(
                destination(&Url::parse(href).unwrap(), &app),
                expected,
                "{href}"
            );
        }
    }

    #[test]
    fn a_secondary_window_is_an_app_page_with_a_fresh_workspace() {
        let app = Url::parse("http://tauri.localhost").unwrap();
        assert_eq!(
            app_page(
                app.join("/?desktop-tab=tab-main-a&host=a%26b#/s/id")
                    .unwrap(),
                &app
            )
            .as_deref(),
            Some("index.html?host=a%26b#/s/id")
        );
        for href in [
            "https://tauri.localhost/",
            "http://tauri.localhost:80@external.example/",
            "http://tauri.localhost/assets/index.js",
            "about:blank",
        ] {
            assert!(
                app_page(Url::parse(href).unwrap(), &app).is_none(),
                "{href}"
            );
        }
    }
}
