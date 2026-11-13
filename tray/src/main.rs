#![cfg_attr(windows, windows_subsystem = "windows")]

use cc_remote_tray::*;
use std::sync::mpsc::{self, SyncSender};
use std::time::Duration;
use windui::prelude::*;

enum Update {
    Snapshot(Snapshot),
    Error(String),
    Quit,
}

fn send(tx: &SyncSender<Action>, action: Action, message: Signal<String>) {
    if tx.try_send(action).is_err() {
        message.set("正在处理，请稍候".into());
    } else {
        message.set("正在读取后台…".into());
    }
}

fn panel(
    snapshot: Signal<Snapshot>,
    focus: Signal<String>,
    message: Signal<String>,
    actions: SyncSender<Action>,
) -> WindowRequest {
    Window::new("CC Remote · 订阅用量", 480, 540)
        .single("usage")
        .centered(true)
        .content(move || {
            let accounts = snapshot.map(|s| s.accounts.clone());
            let interval = snapshot.map(|s| {
                format!(
                    "后台每 {} 分钟采样 · 关闭此窗口后继续记录",
                    s.interval_minutes
                )
            });
            let refresh = actions.clone();
            let ten = actions.clone();
            let thirty = actions.clone();
            let history = actions.clone();
            let rows = actions.clone();
            Element::col()
                .fill()
                .padding(20)
                .spacing(12)
                .child(
                    Element::label("订阅用量")
                        .font_size(22.0)
                        .height(34)
                        .width_match(),
                )
                .child(
                    Element::label(interval)
                        .font_size(12.0)
                        .height(22)
                        .width_match(),
                )
                .child(
                    Element::label(message)
                        .font_size(12.0)
                        .height(34)
                        .width_match()
                        .fg_role(Role::TextMuted),
                )
                .child(
                    Element::row()
                        .height(34)
                        .spacing(8)
                        .child(
                            Element::button("立即刷新")
                                .on_click(move |_| send(&refresh, Action::Refresh, message)),
                        )
                        .child(
                            Element::button("10 分钟")
                                .on_click(move |_| send(&ten, Action::Interval(10), message)),
                        )
                        .child(
                            Element::button("30 分钟")
                                .on_click(move |_| send(&thirty, Action::Interval(30), message)),
                        ),
                )
                .child(
                    Element::list_signal(
                        accounts,
                        |a| a.id.clone(),
                        move |a: Account| {
                            let id = a.id.clone();
                            let selected = a.id.clone();
                            let pin_actions = rows.clone();
                            let history_actions = rows.clone();
                            let focused = focus.map(move |current| {
                                if *current == selected {
                                    "已关注".to_string()
                                } else {
                                    "关注".to_string()
                                }
                            });
                            let history_id = a.id.clone();
                            let updated = format!(
                                "更新：{}{}",
                                a.fetched_at.as_deref().unwrap_or("尚无记录"),
                                if a.identity_known {
                                    ""
                                } else if a.provider == "codex" {
                                    " · 身份不可读，仅显示当前额度"
                                } else {
                                    " · 身份不可读，独立记录"
                                }
                            );
                            Element::col()
                                .width_match()
                                .padding(12)
                                .spacing(6)
                                .child(
                                    Element::label(account_text(&a))
                                        .font_size(14.0)
                                        .height(48)
                                        .width_match(),
                                )
                                .child(
                                    Element::label(updated)
                                        .font_size(11.0)
                                        .height(30)
                                        .width_match()
                                        .fg_role(Role::TextMuted),
                                )
                                .child(
                                    Element::row()
                                        .height(32)
                                        .spacing(8)
                                        .child(Element::button(focused).on_click(move |_| {
                                            focus.set(id.clone());
                                            if let Err(error) = save_focus(&id) {
                                                message.set(error);
                                            } else {
                                                send(&pin_actions, Action::Read, message);
                                            }
                                        }))
                                        .child(Element::button("详细历史").on_click(move |_| {
                                            send(
                                                &history_actions,
                                                Action::History(Some(history_id.clone())),
                                                message,
                                            )
                                        })),
                                )
                        },
                    )
                    .weight(1.0),
                )
                .child(
                    Element::button("打开详细用量历史")
                        .height(34)
                        .on_click(move |_| send(&history, Action::History(None), message)),
                )
        })
}

fn run() -> Result<(), String> {
    let o = Options::parse(std::env::args().skip(1))?;
    if o.check {
        let connection = discover(&o)?;
        let snapshot = MonitorClient::new()?.request(&connection, Action::Read)?;
        println!(
            "{}",
            serde_json::to_string_pretty(&snapshot).map_err(|_| "Invalid snapshot")?
        );
        return Ok(());
    }
    // The offscreen screenshot path has no resident event loop to receive the
    // worker's update. Read the same cache once before constructing that frame.
    let initial = if std::env::args().any(|arg| arg == "--screenshot") {
        MonitorClient::new()?.request(&discover(&o)?, Action::Read)?
    } else {
        Snapshot::default()
    };
    let snapshot = signal(initial);
    let focus = signal(load_focus());
    let message = signal("正在连接 cc-remote 后台；无账号时请在客户端添加登录。".to_string());
    let (actions, commands) = mpsc::sync_channel::<Action>(4);
    let click = actions.clone();
    let menu = actions.clone();
    let refresh = actions.clone();
    let history = actions.clone();
    let tray = Tray::new()
        .tooltip("CC Remote · 正在读取后台")
        .icon_rgba(32, 32, brand_icon_at(32).rgba())
        .on_left_click(move |ctx| ctx.open_window(panel(snapshot, focus, message, click.clone())))
        .menu(vec![
            TrayMenuItem::item("查看订阅用量", move |ctx| {
                ctx.open_window(panel(snapshot, focus, message, menu.clone()))
            }),
            TrayMenuItem::item("立即刷新", move |_| {
                send(&refresh, Action::Refresh, message)
            }),
            TrayMenuItem::item("详细历史", move |_| {
                send(&history, Action::History(None), message)
            }),
            TrayMenuItem::separator(),
            TrayMenuItem::item("退出托盘（后台继续记录）", |ctx| ctx.quit()),
        ]);
    let mut app = App::resident("CC Remote Tray")
        .icon(brand_icon())
        .tray(tray)
        .single_instance("io.github.huanfeng.ccremote.tray", |_| {});
    let handle = app.tray_handle();
    let updates = app.channel::<Update>(move |_ctx, update| match update {
        Update::Snapshot(value) => {
            handle.set_tooltip(tooltip(&value, &focus.get(), None));
            message.set(if value.accounts.is_empty() {
                "尚无采样。请检查本机登录，或在客户端添加 OAuth 授权。".into()
            } else {
                "悬停显示关注账号；详细历史按需打开客户端。".into()
            });
            snapshot.set(value);
        }
        Update::Error(error) => {
            handle.set_tooltip(tooltip(&snapshot.get(), &focus.get(), Some(&error)));
            message.set(error);
        }
        Update::Quit => handle.quit(),
    });
    let quit = updates.clone();
    if let Some(seconds) = o.smoke_seconds {
        std::thread::spawn(move || {
            std::thread::sleep(Duration::from_secs(seconds));
            let _ = quit.send(Update::Quit);
        });
    }
    std::thread::spawn(move || {
        let client = match MonitorClient::new() {
            Ok(c) => c,
            Err(e) => {
                let _ = updates.send(Update::Error(e));
                return;
            }
        };
        let mut next = Action::Read;
        loop {
            let result = discover(&o).and_then(|connection| {
                if let Action::History(account) = next.clone() {
                    open_history(&o, &connection, account)?;
                    client.request(&connection, Action::Read)
                } else {
                    client.request(&connection, next.clone())
                }
            });
            let sent = match result {
                Ok(s) => updates.send(Update::Snapshot(s)),
                Err(e) => updates.send(Update::Error(e)),
            };
            if sent.is_err() {
                return;
            }
            next = match commands.recv_timeout(Duration::from_secs(30)) {
                Ok(action) => action,
                Err(mpsc::RecvTimeoutError::Timeout) => Action::Read,
                Err(_) => return,
            };
        }
    });
    if !app
        .screenshot_window(panel(snapshot, focus, message, actions))
        .screenshot_from_args()
        .run_resident()
    {
        return Err("无法启动系统托盘".into());
    }
    Ok(())
}

fn main() {
    if let Err(error) = run() {
        eprintln!("{error}");
        std::process::exit(1);
    }
}
