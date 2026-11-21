//! One updater shared by all windows and the WebView-free tray.
use serde::Serialize;
use std::{sync::Mutex, time::Duration};
use tauri::{Emitter, Manager};
use tauri_plugin_notification::NotificationExt;
use tauri_plugin_updater::{Update, UpdaterExt};

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct View {
    revision: u64,
    current_version: String,
    phase: &'static str,
    version: Option<String>,
    notes: Option<String>,
    downloaded: u64,
    total: Option<u64>,
    error: Option<String>,
}

struct Inner {
    view: View,
    pending: Option<Update>,
    notified: Option<String>,
}

impl Inner {
    fn new(current_version: String) -> Self {
        Self {
            view: View {
                revision: 0,
                current_version,
                phase: "idle",
                version: None,
                notes: None,
                downloaded: 0,
                total: None,
                error: None,
            },
            pending: None,
            notified: None,
        }
    }

    fn finish_check(
        &mut self,
        result: Result<Option<Update>, String>,
        automatic: bool,
        previous: View,
    ) -> Option<String> {
        match result {
            Ok(Some(update)) => {
                self.view.phase = "available";
                self.view.version = Some(update.version.clone());
                self.view.notes = update.body.clone();
                let should_notify = automatic && self.notified.as_ref() != Some(&update.version);
                self.pending = Some(update);
                if should_notify {
                    self.notified = self.view.version.clone();
                    return self.notified.clone();
                }
            }
            Ok(None) => {
                self.pending = None;
                self.view.phase = "updated";
                self.view.version = None;
                self.view.notes = None;
            }
            Err(_) if automatic => {
                // Keep a known update visible through a background network failure.
                self.view.phase = previous.phase;
                self.view.error = previous.error;
            }
            Err(error) => {
                self.view.phase = "error";
                self.view.error = Some(error);
            }
        }
        None
    }
}

pub struct DesktopUpdate {
    inner: Mutex<Inner>,
    operation: tokio::sync::Mutex<()>,
}

fn change(app: &tauri::AppHandle, edit: impl FnOnce(&mut Inner)) -> View {
    let state = app.state::<DesktopUpdate>();
    let mut inner = state.inner.lock().unwrap();
    let previous_version = inner.view.version.clone();
    edit(&mut inner);
    inner.view.revision += 1;
    let view = inner.view.clone();
    // Emit under the same lock so windows see transitions in order.
    let _ = app.emit("desktop-update", &view);
    drop(inner);
    if previous_version != view.version {
        let handle = app.clone();
        let _ = app.run_on_main_thread(move || {
            let current = desktop_update_status(handle.clone());
            crate::usage_tray::update_indicator(&handle, current.version.as_deref());
        });
    }
    view
}

#[tauri::command]
pub fn desktop_update_status(app: tauri::AppHandle) -> View {
    app.state::<DesktopUpdate>()
        .inner
        .lock()
        .unwrap()
        .view
        .clone()
}

fn configured(app: &tauri::AppHandle) -> bool {
    app.config()
        .plugins
        .0
        .get("updater")
        .and_then(|v| v.get("pubkey"))
        .and_then(|v| v.as_str())
        .is_some_and(|v| !v.trim().is_empty())
}

async fn check(app: &tauri::AppHandle, automatic: bool) -> View {
    let state = app.state::<DesktopUpdate>();
    let _guard = if automatic {
        state.operation.lock().await
    } else {
        let Ok(guard) = state.operation.try_lock() else {
            return desktop_update_status(app.clone());
        };
        guard
    };
    let previous = desktop_update_status(app.clone());
    change(app, |inner| {
        inner.view.phase = "checking";
        inner.view.error = None;
        inner.view.downloaded = 0;
        inner.view.total = None;
    });
    let result = async {
        if !cfg!(all(windows, target_arch = "x86_64")) {
            return Err("Automatic desktop updates are currently available on Windows x64 only.".to_string());
        }
        if !configured(app) {
            return Err("This build has no update verification key. Install an official signed release to enable updates.".to_string());
        }
        let directory = std::env::current_exe().map_err(|e| e.to_string())?
            .parent().ok_or("Cannot locate the desktop installation")?.to_path_buf();
        // NSIS consumes /D as its final argument, including spaces. This also
        // updates clients installed by install-desktop.ps1 in cc-remote/bin.
        let updater = app.updater_builder()
            .timeout(Duration::from_secs(30))
            .installer_arg(format!("/D={}", directory.display()))
            .build().map_err(|e| e.to_string())?;
        updater.check().await.map_err(|e| e.to_string())
    }.await;
    let mut notification = None;
    let view = change(app, |inner| {
        notification = inner.finish_check(result, automatic, previous);
    });
    if let Some(version) = notification {
        let _ = app
            .notification()
            .builder()
            .title("CC Remote 有新版本")
            .body(format!("桌面端 {version} 已发布，请打开客户端安装更新。"))
            .show();
    }
    view
}

#[tauri::command]
pub async fn desktop_update_check(app: tauri::AppHandle) -> View {
    check(&app, false).await
}

#[tauri::command]
pub async fn desktop_update_install(app: tauri::AppHandle) -> Result<(), String> {
    let state = app.state::<DesktopUpdate>();
    let _guard = state
        .operation
        .try_lock()
        .map_err(|_| "An update operation is already running")?;
    let mut update = state
        .inner
        .lock()
        .unwrap()
        .pending
        .clone()
        .ok_or("Check for a new desktop version before installing")?;
    update.timeout = Some(Duration::from_secs(600));
    change(&app, |inner| {
        inner.view.phase = "downloading";
        inner.view.error = None;
        inner.view.downloaded = 0;
        inner.view.total = None;
    });
    // download() verifies the signature before install() is ever called.
    let result = update
        .download(
            |size, total| {
                change(&app, |inner| {
                    inner.view.downloaded += size as u64;
                    inner.view.total = total;
                });
            },
            || {},
        )
        .await;
    let result = match result {
        Ok(bytes) => {
            change(&app, |inner| inner.view.phase = "installing");
            // Extraction and launching the installer must not block async workers.
            tauri::async_runtime::spawn_blocking(move || update.install(bytes))
                .await
                .map_err(|e| e.to_string())
                .and_then(|r| r.map_err(|e| e.to_string()))
        }
        Err(error) => Err(error.to_string()),
    };
    if let Err(error) = &result {
        change(&app, |inner| {
            inner.view.phase = "error";
            inner.view.error = Some(error.clone());
        });
    }
    result
}

pub fn setup(app: &tauri::AppHandle) {
    app.manage(DesktopUpdate {
        inner: Mutex::new(Inner::new(app.package_info().version.to_string())),
        operation: tokio::sync::Mutex::new(()),
    });
    if !configured(app) || !cfg!(all(windows, target_arch = "x86_64")) {
        return;
    }
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        // Also runs in --tray mode, before any WebView is created.
        tokio::time::sleep(Duration::from_secs(15)).await;
        loop {
            check(&app, true).await;
            tokio::time::sleep(Duration::from_secs(6 * 60 * 60)).await;
        }
    });
}

#[cfg(test)]
mod tests {
    use super::*;

    fn signed_fixture() -> tauri_plugin_updater::Updater {
        let mut context = tauri::test::mock_context(tauri::test::noop_assets());
        context.config_mut().plugins.0.insert(
            "updater".into(),
            serde_json::json!({
                "pubkey": include_str!("../tests/fixtures/update.pub").trim(),
                "requireSignedVersion": true,
            "endpoints": ["https://example.com/latest.json"],
            }),
        );
        let app = tauri::test::mock_builder()
            .plugin(tauri_plugin_updater::Builder::new().build())
            .build(context)
            .unwrap();
        app.updater_builder()
            .target("windows-x86_64")
            .build()
            .unwrap()
    }

    fn manifest(version: &str) -> serde_json::Value {
        serde_json::json!({
            "version": version,
            "platforms": { "windows-x86_64": {
                "url": "https://example.com/installer.exe",
                "signature": include_str!("../tests/fixtures/update.txt.sig").trim(),
            }},
        })
    }

    #[test]
    fn signed_updates_reject_tampered_bytes_and_mislabelled_versions() {
        let updater = signed_fixture();
        let update = updater.restore_update(manifest("99.0.0")).unwrap().unwrap();
        let bytes = include_bytes!("../tests/fixtures/update.txt");
        update
            .verify(bytes)
            .expect("fixture has a valid release signature");
        assert!(update.verify(b"tampered installer").is_err());
        let wrong_version = updater.restore_update(manifest("99.0.1")).unwrap().unwrap();
        assert!(
            wrong_version.verify(bytes).is_err(),
            "manifest cannot relabel an old signed artifact"
        );
        assert!(
            updater.restore_update(manifest("0.0.0")).unwrap().is_none(),
            "older versions are not offered"
        );
    }

    #[test]
    fn automatic_checks_notify_once_and_keep_a_known_update_when_offline() {
        let updater = signed_fixture();
        let update = updater.restore_update(manifest("99.0.0")).unwrap().unwrap();
        let mut inner = Inner::new("1.0.0".into());
        assert!(inner
            .finish_check(Ok(Some(update.clone())), false, inner.view.clone())
            .is_none());
        assert_eq!(
            inner
                .finish_check(Ok(Some(update.clone())), true, inner.view.clone())
                .as_deref(),
            Some("99.0.0"),
        );
        assert!(inner
            .finish_check(Ok(Some(update)), true, inner.view.clone())
            .is_none());
        assert!(inner
            .finish_check(Err("offline".into()), true, inner.view.clone())
            .is_none());
        assert_eq!(inner.view.phase, "available");
        assert_eq!(inner.view.version.as_deref(), Some("99.0.0"));
        assert!(inner.pending.is_some());
        assert!(inner.view.error.is_none());
        inner.finish_check(Ok(None), true, inner.view.clone());
        assert!(inner.view.version.is_none());
        assert!(inner.pending.is_none());
    }
}
