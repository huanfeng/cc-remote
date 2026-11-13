//! Tauri owns the icon; windui owns its independent native UI thread.
use super::View;

pub struct Handle {
    #[cfg(windows)]
    tx: std::sync::mpsc::Sender<Event>,
}
impl Handle {
    pub fn start(app: tauri::AppHandle) -> Self {
        #[cfg(windows)]
        {
            let (tx, rx) = std::sync::mpsc::channel();
            std::thread::Builder::new()
                .name("usage-hover".into())
                .spawn(move || native::run(app, rx))
                .expect("usage hover coordinator");
            Self { tx }
        }
        #[cfg(not(windows))]
        {
            let _ = app;
            Self {}
        }
    }
    pub fn update(&self, view: &View) {
        #[cfg(windows)]
        let _ = self.tx.send(Event::Update(Box::new(view.clone())));
        #[cfg(not(windows))]
        let _ = view;
    }
    pub fn enter(&self, x: i32, y: i32, w: i32, h: i32) {
        #[cfg(windows)]
        let _ = self.tx.send(Event::Enter(Rect {
            left: x,
            top: y,
            right: x + w,
            bottom: y + h,
        }));
        #[cfg(not(windows))]
        let _ = (x, y, w, h);
    }
    pub fn leave(&self) {
        #[cfg(windows)]
        let _ = self.tx.send(Event::Leave);
    }
    pub fn suppress(&self, value: bool) {
        #[cfg(windows)]
        let _ = self.tx.send(Event::Suppress(value));
        #[cfg(not(windows))]
        let _ = value;
    }
}
impl Drop for Handle {
    fn drop(&mut self) {
        #[cfg(windows)]
        let _ = self.tx.send(Event::Stop);
    }
}

#[cfg(windows)]
enum Event {
    Update(Box<View>),
    Enter(Rect),
    Leave,
    Suppress(bool),
    Stop,
}

/// Pure transition logic: delayed entry, a short crossing grace and suppression
/// until leaving both surfaces. The coordinator sleeps indefinitely when idle.
#[cfg(any(windows, test))]
#[derive(Default)]
struct Dwell {
    entered: Option<u64>,
    left: Option<u64>,
    suppressed: bool,
    shown: bool,
}
#[cfg(any(windows, test))]
impl Dwell {
    fn update(&mut self, now: u64, icon: bool, panel: bool, blocked: bool) -> bool {
        if blocked {
            self.suppressed = true;
            self.shown = false;
            self.entered = None;
        }
        if !icon && !panel {
            self.suppressed = false;
            self.entered = None;
            let left = *self.left.get_or_insert(now);
            if now.saturating_sub(left) >= 200 {
                self.shown = false;
            }
        } else {
            self.left = None;
            if icon && !self.suppressed && !blocked {
                let entered = *self.entered.get_or_insert(now);
                if now.saturating_sub(entered) >= 400 {
                    self.shown = true;
                }
            }
        }
        self.shown
    }
    fn active(&self) -> bool {
        self.entered.is_some() || self.shown || self.suppressed
    }
}
#[cfg(windows)]
#[derive(Clone, Copy, Default)]
#[repr(C)]
struct Rect {
    left: i32,
    top: i32,
    right: i32,
    bottom: i32,
}

#[cfg(windows)]
mod native {
    use super::*;
    use crate::usage_tray::{panel, UsageTray};
    use std::{
        ffi::c_void,
        sync::mpsc,
        time::{Duration, Instant},
    };
    use tauri::Manager;
    use windui::prelude::{signal, App};
    const TITLE: &str = "CC Remote Desktop · 订阅用量";
    const KEY: &str = "desktop-usage-native";
    enum Message {
        Update(Box<View>),
        Open,
        Close,
        Quit,
    }
    #[derive(Default)]
    #[repr(C)]
    struct Point {
        x: i32,
        y: i32,
    }
    #[link(name = "user32")]
    extern "system" {
        fn GetCursorPos(point: *mut Point) -> i32;
        fn GetWindowRect(hwnd: *mut c_void, rect: *mut Rect) -> i32;
        fn FindWindowW(class: *const u16, title: *const u16) -> *mut c_void;
        fn GetWindowThreadProcessId(hwnd: *mut c_void, pid: *mut u32) -> u32;
        fn IsWindowVisible(hwnd: *mut c_void) -> i32;
        fn GetAsyncKeyState(key: i32) -> i16;
        fn SetThreadDpiAwarenessContext(context: *mut c_void) -> *mut c_void;
        fn SetWindowPos(
            hwnd: *mut c_void,
            after: *mut c_void,
            x: i32,
            y: i32,
            w: i32,
            h: i32,
            flags: u32,
        ) -> i32;
    }
    fn contains(rect: Rect, p: &Point) -> bool {
        p.x >= rect.left && p.x < rect.right && p.y >= rect.top && p.y < rect.bottom
    }
    fn window() -> Option<*mut c_void> {
        let title: Vec<u16> = TITLE.encode_utf16().chain(Some(0)).collect();
        unsafe {
            let hwnd = FindWindowW(std::ptr::null(), title.as_ptr());
            let mut pid = 0;
            if !hwnd.is_null()
                && GetWindowThreadProcessId(hwnd, &mut pid) != 0
                && pid == std::process::id()
                && IsWindowVisible(hwnd) != 0
            {
                Some(hwnd)
            } else {
                None
            }
        }
    }
    pub(super) fn run(app: tauri::AppHandle, rx: mpsc::Receiver<Event>) {
        unsafe {
            SetThreadDpiAwarenessContext(-4isize as *mut c_void);
        }
        let (ready_tx, ready_rx) = mpsc::sync_channel(1);
        let (actions, action_rx) = mpsc::channel();
        let action_app = app.clone();
        std::thread::Builder::new()
            .name("usage-actions".into())
            .spawn(move || {
                for action in action_rx {
                    match action {
                        panel::Action::Refresh => {
                            if let Err(error) = super::super::request_refresh(&action_app) {
                                feedback(&action_app, error);
                            }
                        }
                        panel::Action::Favorite(id, favorite) => {
                            if let Err(error) =
                                super::super::set_favorite(action_app.clone(), id, favorite)
                            {
                                feedback(&action_app, error);
                            }
                        }
                        panel::Action::History(id) => {
                            action_app.state::<UsageTray>().hover.suppress(true);
                            super::super::open_history(&action_app, id);
                        }
                        panel::Action::Hide => action_app.state::<UsageTray>().hover.suppress(true),
                        panel::Action::ShowMain => {
                            action_app.state::<UsageTray>().hover.suppress(true);
                            super::super::show_main(&action_app);
                        }
                    }
                }
            })
            .expect("usage action worker");
        let ui = std::thread::Builder::new()
            .name("usage-native-ui".into())
            .spawn(move || {
                unsafe {
                    SetThreadDpiAwarenessContext(-4isize as *mut c_void);
                }
                let model = signal(panel::Model::default());
                let mut native = App::resident("CC Remote usage");
                let quit = native.tray_handle();
                let tx = native.channel(move |ctx, msg| match msg {
                    Message::Update(view) => {
                        model.update(|m| m.apply(*view, windui::event::window_open(KEY)))
                    }
                    Message::Open => ctx.open_window(panel::panel(model, actions.clone())),
                    Message::Close => {
                        model.update(|m| m.clear_feedback());
                        ctx.close_window(KEY);
                    }
                    Message::Quit => quit.quit(),
                });
                if ready_tx.send(tx).is_ok() {
                    native.run_resident();
                }
            })
            .expect("usage native UI");
        let Ok(tx) = ready_rx.recv() else {
            return;
        };
        let mut dwell = Dwell::default();
        let mut icon = Rect::default();
        let mut active = false;
        let mut shown = false;
        let mut topmost = false;
        let mut last_open = 0;
        let start = Instant::now();
        loop {
            let event = if active {
                rx.recv_timeout(Duration::from_millis(40))
            } else {
                rx.recv().map_err(|_| mpsc::RecvTimeoutError::Disconnected)
            };
            match event {
                Ok(Event::Update(view)) => {
                    if tx.send(Message::Update(view)).is_err() {
                        break;
                    }
                }
                Ok(Event::Enter(rect)) => {
                    icon = rect;
                    active = true;
                }
                Ok(Event::Leave) => {} // Physical cursor also covers the icon-to-window gap.
                Ok(Event::Suppress(value)) => {
                    if value {
                        dwell.suppressed = true;
                        dwell.shown = false;
                        dwell.entered = None;
                    } else {
                        dwell.suppressed = false;
                    }
                    active = true;
                }
                Ok(Event::Stop) | Err(mpsc::RecvTimeoutError::Disconnected) => break,
                Err(mpsc::RecvTimeoutError::Timeout) => {}
            }
            if !active {
                continue;
            }
            let mut cursor = Point::default();
            let mut bounds = Rect::default();
            let popup = window();
            let (on_icon, on_panel, escape) = unsafe {
                let valid = GetCursorPos(&mut cursor) != 0;
                let in_panel = popup.is_some_and(|hwnd| {
                    GetWindowRect(hwnd, &mut bounds) != 0 && contains(bounds, &cursor)
                });
                // Keep native title dragging alive until the mouse button is released.
                (
                    valid && contains(icon, &cursor),
                    valid && (in_panel || (shown && GetAsyncKeyState(1) < 0)),
                    shown && GetAsyncKeyState(0x1b) < 0,
                )
            };
            let now = start.elapsed().as_millis() as u64;
            let want = dwell.update(now, on_icon, on_panel, escape);
            if want != shown {
                if tx
                    .send(if want { Message::Open } else { Message::Close })
                    .is_err()
                {
                    break;
                }
                shown = want;
                if want {
                    last_open = now;
                }
                topmost = false;
            }
            // Native close is posted asynchronously. If a delayed close and a
            // later enter were drained together, retry after destruction rather
            // than retaining a logical open state with no actual window.
            if shown && popup.is_none() && on_icon && now.saturating_sub(last_open) >= 600 {
                if tx.send(Message::Open).is_err() {
                    break;
                }
                last_open = now;
                topmost = false;
            }
            if shown && !topmost {
                if let Some(hwnd) = popup {
                    // Above ordinary windows without taking keyboard focus.
                    unsafe {
                        SetWindowPos(hwnd, -1isize as *mut c_void, 0, 0, 0, 0, 0x0013);
                    }
                    topmost = true;
                }
            }
            active = dwell.active();
        }
        let _ = tx.send(Message::Quit);
        let _ = ui.join();
    }
    fn feedback(app: &tauri::AppHandle, error: String) {
        {
            let state = app.state::<UsageTray>();
            let mut view = state.view.lock().unwrap();
            view.feedback_id += 1;
            view.feedback = Some(error);
            view.feedback_error = true;
        }
        super::super::publish(app);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn hover_delays_entry_bridges_gap_and_suppresses_clicks() {
        let mut d = Dwell::default();
        assert!(!d.active());
        assert!(!d.update(0, true, false, false));
        assert!(!d.update(399, true, false, false));
        assert!(d.update(400, true, false, false));
        assert!(d.update(450, false, false, false));
        assert!(d.update(550, false, true, false));
        assert!(!d.update(600, true, false, true));
        assert!(!d.update(1200, true, false, false));
        assert!(!d.update(1300, false, false, false));
        assert!(!d.update(1400, true, false, false));
        assert!(d.update(1800, true, false, false));
        assert!(d.update(1900, false, false, false));
        assert!(!d.update(2100, false, false, false));
        assert!(!d.active());
    }
    #[test]
    fn entering_panel_during_grace_keeps_it_open_until_leaving_both() {
        let mut d = Dwell::default();
        d.update(0, true, false, false);
        d.update(400, true, false, false);
        assert!(d.update(410, false, false, false));
        assert!(d.update(580, false, true, false));
        assert!(d.update(5000, false, true, false));
        assert!(d.update(5100, false, false, false));
        assert!(!d.update(5300, false, false, false));
        assert!(!d.active());
    }
}
