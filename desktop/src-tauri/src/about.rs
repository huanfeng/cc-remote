//! Keep tray navigation pending until the main WebView is ready to receive it.
use std::sync::atomic::{AtomicBool, Ordering};
use tauri::{State, Webview};

#[derive(Default)]
pub struct Requests(AtomicBool);

impl Requests {
    pub fn request(&self) {
        self.0.store(true, Ordering::SeqCst);
    }

    fn take(&self, window: &str) -> bool {
        window == "main" && self.0.swap(false, Ordering::SeqCst)
    }
}

#[tauri::command]
pub fn take_desktop_about_request(window: Webview, requests: State<'_, Requests>) -> bool {
    requests.take(window.label())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn tray_request_survives_webview_startup_and_is_consumed_once_by_main() {
        let requests = Requests::default();
        assert!(!requests.take("main"));
        requests.request();
        assert!(!requests.take("host-1"));
        assert!(requests.take("main"));
        assert!(!requests.take("main"));
    }

    #[test]
    fn repeated_requests_before_startup_open_one_about_dialog() {
        let requests = Requests::default();
        requests.request();
        requests.request();
        assert!(requests.take("main"));
        assert!(!requests.take("main"));
    }
}
