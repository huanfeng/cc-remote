use super::{model::*, View};
use std::sync::mpsc::Sender;
use std::time::{Duration, Instant};
use windui::core::{EventCtx, Widget};
use windui::prelude::*;
use windui::render::{Canvas, Paint};

#[derive(Clone, Debug, PartialEq)]
pub(super) enum Action {
    Refresh,
    Favorite(String, bool),
    History(Option<String>),
    Hide,
    ShowMain,
}

#[derive(Clone, Default)]
pub(super) struct Model {
    pub view: View,
    pub revision: u64,
    feedback: Option<Feedback>,
}
#[derive(Clone)]
struct Feedback {
    text: String,
    kind: windui::event::ToastKind,
    created: Instant,
}
impl Model {
    /// Hidden background results must never replay as notices when reopened.
    pub fn apply(&mut self, view: View, visible: bool) {
        if !visible {
            self.feedback = None;
        }
        if view.feedback_id != self.view.feedback_id {
            self.feedback = None;
            if visible {
                if let Some(text) = &view.feedback {
                    self.feedback(text.clone(), view.feedback_error);
                }
            }
        }
        self.view = view;
    }
    pub fn clear_feedback(&mut self) {
        self.feedback = None;
    }

    fn feedback(&mut self, text: impl Into<String>, error: bool) {
        self.feedback = Some(Feedback {
            text: text.into(),
            kind: if error {
                windui::event::ToastKind::Error
            } else {
                windui::event::ToastKind::Success
            },
            created: Instant::now(),
        });
    }
    fn refresh_label(&self) -> String {
        if self.view.loading || self.view.busy {
            if self.view.loading {
                "连接中"
            } else {
                "刷新中"
            }
            .into()
        } else if self.view.connected {
            "刷新用量".into()
        } else {
            "重新连接".into()
        }
    }
    fn rows(&self) -> Vec<AccountRow> {
        let mut accounts: Vec<_> = self
            .view
            .snapshot
            .accounts
            .iter()
            .map(|a| AccountRow {
                account: a.clone(),
                pinned: self.view.favorites.contains(&a.id),
                offline: !self.view.connected,
                revision: self.revision,
            })
            .collect();
        accounts.sort_by_key(|r| (!r.pinned, !r.account.active));
        accounts
    }
}

/// Keep native button input/focus behavior; animate only a tiny vector icon.
struct RefreshButton {
    button: windui::ui::Button,
    model: Signal<Model>,
}
impl RefreshButton {
    fn new(model: Signal<Model>) -> Self {
        let mut button = windui::ui::Button::new(model.map(Model::refresh_label));
        button.set_intent(windui::theme::Intent::Neutral);
        button.set_variant(windui::ui::ButtonVariant::OutlineSoft);
        Self { button, model }
    }

    fn arc(bounds: Rect, now: u64) -> [(f32, f32); 25] {
        let phase = (now % 900) as f32 / 900.0 * std::f32::consts::TAU;
        std::array::from_fn(|i| {
            let angle = phase + i as f32 / 24.0 * std::f32::consts::TAU * 0.75;
            (
                bounds.x as f32 + 14.0 + 5.0 * angle.cos(),
                bounds.y as f32 + bounds.h as f32 / 2.0 + 5.0 * angle.sin(),
            )
        })
    }
}
impl Widget for RefreshButton {
    fn measure(&self, avail: Size, style: &Style, text: &mut dyn windui::text::TextEngine) -> Size {
        self.button.measure(avail, style, text)
    }
    fn frame_corner(&self, bounds: Rect) -> Option<f32> {
        self.button.frame_corner(bounds)
    }
    fn paint(
        &self,
        bounds: Rect,
        content: Rect,
        focused: bool,
        enabled: bool,
        canvas: &mut dyn Canvas,
        style: &Style,
    ) {
        self.button
            .paint(bounds, content, focused, enabled, canvas, style);
        if !self.model.with(|m| m.view.loading || m.view.busy) {
            return;
        }
        let moving = windui::anim::movement_enabled();
        let points = Self::arc(bounds, if moving { windui::anim::clock_ms() } else { 0 });
        let paint = Paint::fill(Role::Accent.resolve(&windui::theme::current()));
        canvas.draw_polyline(&points, 1.6, &paint);
        for &(x, y) in [points[0], points[24]].iter() {
            canvas.fill_circle(x, y, 0.8, &paint);
        }
        if moving {
            windui::anim::request_repaint_in(Rect::new(
                bounds.x + 7,
                bounds.y + bounds.h / 2 - 7,
                14,
                14,
            ));
        }
    }
    fn on_event(&mut self, ctx: &mut EventCtx, event: &windui::event::Event) -> bool {
        self.button.on_event(ctx, event)
    }
    fn focusable(&self) -> bool {
        self.button.focusable()
    }
    fn take_click(&mut self, callback: windui::core::ClickFn) {
        self.button.take_click(callback);
    }
    fn reset_interaction(&mut self) {
        self.button.reset_interaction();
    }
}

/// Forward feedback to windui's host overlay without a node in the content flow.
struct FeedbackSink(Signal<Model>);
impl Widget for FeedbackSink {
    fn on_update(&mut self, ctx: &mut EventCtx) {
        if self.0.get().feedback.is_some() {
            let mut feedback = None;
            self.0.update(|m| feedback = m.feedback.take());
            if let Some(feedback) =
                feedback.filter(|f| f.created.elapsed() < Duration::from_secs(5))
            {
                ctx.toast_with(
                    feedback.text,
                    feedback.kind,
                    feedback.kind.default_duration_ms(),
                );
            }
        }
    }
}

#[derive(Clone, PartialEq)]
struct AccountRow {
    account: Account,
    pinned: bool,
    offline: bool,
    revision: u64,
}

fn send(tx: &Sender<Action>, action: Action, model: Signal<Model>) {
    let refresh = matches!(action, Action::Refresh);
    if refresh && (model.get().view.busy || model.get().view.loading) {
        return;
    }
    if tx.send(action).is_ok() {
        if refresh {
            model.update(|m| {
                m.view.busy = true;
                m.feedback = None;
            });
        }
    } else {
        model.update(|m| m.feedback("操作队列暂时不可用，请稍后重试", true));
    }
}

fn text(value: impl Into<TextContent>, size: f32, height: i32) -> Element {
    Element::label(value)
        .font_size(size)
        .height(height)
        .width_match()
        .max_lines(1)
        .truncate(Truncate::End)
}

fn muted(value: impl Into<TextContent>, height: i32) -> Element {
    text(value, 12.0, height).fg_role(Role::TextMuted)
}

fn quota_role(percent: Option<f64>) -> Role {
    match percent {
        Some(p) if p >= 90.0 => Role::Danger,
        Some(p) if p >= 75.0 => Role::Warning,
        _ => Role::Accent,
    }
}

/// Static, theme-aware usage bars. The adjacent text carries value and meaning.
struct QuotaBar(Option<f64>, bool);

impl Widget for QuotaBar {
    fn paint(&self, bounds: Rect, _: Rect, _: bool, _: bool, canvas: &mut dyn Canvas, _: &Style) {
        let theme = windui::theme::current();
        let (x, y, w) = (bounds.x as f32, bounds.y as f32, bounds.w as f32);
        canvas.fill_round_rect(x, y, w, 6.0, 3.0, &Paint::fill(theme.palette.surface_alt));
        if let Some(percent) = self.0 {
            let color = if self.1 {
                theme.palette.text_subtle
            } else {
                quota_role(self.0).resolve(&theme)
            };
            let width = w * (percent as f32 / 100.0);
            if width > 0.0 {
                canvas.fill_round_rect(x, y, width, 6.0, 3.0, &Paint::fill(color));
            }
        }
    }
}

fn quota(window: &QuotaWindow, stale: bool) -> Element {
    let percent = quota_percent(window);
    Element::col()
        .width_match()
        .spacing(2)
        .child(
            Element::row()
                .height(17)
                .child(
                    text(window_label(window), 11.0, 17)
                        .weight(1.0)
                        .fg(Color::hex(quota_label_color(window))),
                )
                .child(
                    Element::label(percent_text(window))
                        .font_size(12.0)
                        .font_weight(600)
                        .fg_role(if stale {
                            Role::TextMuted
                        } else {
                            quota_role(percent)
                        }),
                ),
        )
        .child(
            Element::col()
                .height(6)
                .width_match()
                .widget(QuotaBar(percent, stale)),
        )
        .child(muted(reset_text(window), 15).font_size(10.0))
}

fn account_card(row: AccountRow, model: Signal<Model>, actions: Sender<Action>) -> Element {
    let a = row.account;
    let id = a.id.clone();
    let history_id = a.id.clone();
    let stale = row.offline || a.stale || !a.active || !a.available || a.error.is_some();
    let favorites = actions.clone();
    let status = if row.offline {
        "后台离线 · 显示上次记录".into()
    } else {
        account_status(&a)
    };
    let mut card = Element::col()
        .width_match()
        .padding(8)
        .spacing(4)
        .bg_role(Role::Surface)
        .border_role(
            if row.pinned {
                Role::Accent
            } else {
                Role::Divider
            },
            1,
        )
        .corner(10.0)
        .child(
            Element::row()
                .height(22)
                .spacing(6)
                .child(
                    text(account_name(&a), 12.0, 22)
                        .font_weight(600)
                        .weight(1.0)
                        .tooltip(format!(
                            "{} · {}\n{}",
                            a.name,
                            a.plan.as_deref().unwrap_or("套餐未知"),
                            updated_text(&a)
                        )),
                )
                .child(
                    Element::button("历史")
                        .small()
                        .neutral()
                        .outline_soft()
                        .height(22)
                        .enabled_when(move || !model.get().view.busy)
                        .on_click(move |_| {
                            send(&actions, Action::History(Some(history_id.clone())), model);
                        }),
                )
                .child(
                    Element::icon_button(if row.pinned { "★" } else { "☆" })
                        .size(22, 22)
                        .fg_role(if row.pinned {
                            Role::AccentText
                        } else {
                            Role::TextMuted
                        })
                        .tooltip(if row.pinned {
                            "取消收藏"
                        } else {
                            "收藏并置顶（可多选）"
                        })
                        .on_click(move |_| {
                            let favorite = !model.get().view.favorites.contains(&id);
                            send(&favorites, Action::Favorite(id.clone(), favorite), model);
                        }),
                ),
        );
    for pair in a.windows.chunks(2) {
        let mut row = Element::row().width_match().spacing(14);
        for window in pair {
            row = row.child(quota(window, stale).weight(1.0));
        }
        card = card.child(row);
    }
    if a.windows.is_empty()
        || a.error.is_some()
        || !a.available
        || !a.active
        || !a.identity_known
        || row.offline
        || a.stale
    {
        card = card.child(
            muted(status, 18)
                .fg_role(if stale {
                    Role::Warning
                } else {
                    Role::TextMuted
                })
                .tooltip(a.error.clone().unwrap_or_else(|| account_status(&a))),
        );
    }
    card
}

pub(super) fn panel(model: Signal<Model>, actions: Sender<Action>) -> WindowRequest {
    // Reserve the normal viewport during startup; shrink known single/empty
    // states when reopened, without resizing a window under the user's pointer.
    let current = model.get().view;
    let desired_height = if current.loading {
        272
    } else if current.snapshot.accounts.is_empty() {
        268
    } else {
        let content: i32 = current
            .snapshot
            .accounts
            .iter()
            .map(|a| {
                90 + a.windows.len().saturating_sub(2).div_ceil(2) as i32 * 42
                    + if a.error.is_some()
                        || a.stale
                        || !a.available
                        || !a.active
                        || !a.identity_known
                        || !current.connected
                    {
                        22
                    } else {
                        0
                    }
            })
            .sum();
        (74 + content).clamp(168, 480)
    };
    let (x, y, height) = super::placement::popup_bounds(440, desired_height);
    let dismiss = actions.clone();
    Window::new("CC Remote Desktop · 订阅用量", 440, height)
        .single("desktop-usage-native")
        .frameless(true)
        .skip_taskbar(true)
        .activate_on_show(false)
        .resizable(false)
        .position(x, y)
        .icon({
            let image = windui::render::image::Image::from_png_bytes(include_bytes!(
                "../../icons/icon.png"
            ))
            .expect("embedded CC Remote PNG");
            windui::icon::WindowIcon::from_image(&image).expect("valid CC Remote icon")
        })
        .on_shortcut(move |_, event| {
            if event.pressed && event.key == Key::Escape {
                send(&dismiss, Action::Hide, model);
                true
            } else {
                false
            }
        })
        .on_interval(Duration::from_secs(30), move |_| {
            model.update(|m| m.revision += 1)
        })
        .content(move || panel_content(model, actions))
}

fn panel_content(model: Signal<Model>, actions: Sender<Action>) -> Element {
    let refresh = actions.clone();
    let history = actions.clone();
    let empty_history = actions.clone();
    let rows = actions.clone();
    let dismiss = actions.clone();
    Element::col().fill().padding(10).spacing(6).font_family("Microsoft YaHei UI")
                .widget(FeedbackSink(model)).reactive()
                .bg_role(Role::Bg).border_role(Role::Divider, 1)
                .child(Element::row().height(28).spacing(8).window_drag()
                    .child(Element::col().weight(1.0)
                        .child(text("订阅用量", 16.0, 28).font_weight(650)))
                    .child(Element::button("全部历史").small().neutral().outline_soft()
                        .enabled_when(move || !model.get().view.busy)
                        .on_click(move |_| send(&history, Action::History(None), model)))
                    .child(Element::leaf().widget(RefreshButton::new(model))
                        .font_size(12.0).width(88).height(28)
                        .enabled_when(move || !model.get().view.busy && !model.get().view.loading)
                        .on_click(move |_| {
                            send(&refresh, Action::Refresh, model);
                        }))
                    .child(Element::icon_button("×").size(26, 26).tooltip("收起 (Esc)").on_click(move |_| send(&dismiss, Action::Hide, model))))
                .child(Element::row().height(16).spacing(6)
                    .child(Element::label("●").font_size(9.0).width(12)
                        .fg_role_signal(model.map(|m| if m.view.connected { Role::Success } else { Role::Warning })))
                    .child(muted(model.map(|m| {
                        if m.view.loading { "正在连接本机后台…".into() }
                        else if m.view.connected { format!("自动记录中 · {} 个账号", m.view.snapshot.accounts.iter().filter(|a| a.active).count()) }
                        else if m.view.snapshot.accounts.is_empty() { "后台离线".into() }
                        else { "后台离线 · 保留上次读数".into() }
                    }), 16).font_size(11.0).weight(1.0)))
                .child(Element::list_signal(model.map(Model::rows), |r| r.account.id.clone(), move |r| {
                    Element::col().width_match().padding_edges(Insets::new(0, 0, 8, 4))
                        .child(account_card(r, model, rows.clone()))
                }).weight(1.0).visible_when(move || !model.get().view.snapshot.accounts.is_empty()))
                .child(Element::scroll().weight(1.0).width_match()
                    .visible_when(move || model.get().view.snapshot.accounts.is_empty())
                    .child(Element::col().width_match().padding(14).spacing(10).bg_role(Role::Surface).corner(10.0)
                    .child(text(model.map(|m| if m.view.loading { "准备你的用量概览" } else if m.view.connected { "还没有订阅用量" } else { "连接暂时中断" }.to_string()), 17.0, 30).font_weight(600))
                    .child(muted(model.map(|m| {
                        if m.view.loading { "正在读取后台缓存，稍后会在这里显示账号。".into() }
                        else if m.view.connected { "请先登录 Claude / Codex，或在客户端添加 OAuth 账号。后台会自动发现并记录。".into() }
                        else { format!("{}\n确认本机后台正在运行，重新连接后可继续查看。", m.view.error.as_deref().unwrap_or("无法读取后台用量")) }
                    }), 64).max_lines(3))
                    .child(Element::button(model.map(|m| if m.view.connected { "打开客户端" } else { "重新连接" }.to_string()))
                        .small().outline_soft().enabled_when(move || !model.get().view.loading && !model.get().view.busy).on_click(move |_| {
                        if model.get().view.connected {
                            send(&empty_history, Action::ShowMain, model);
                        } else { send(&empty_history, Action::Refresh, model); }
                    }))))
}

#[cfg(test)]
mod tests {
    use super::*;
    use windui::{core::Tree, text::NullTextEngine};

    fn populated() -> View {
        View {
            connected: true,
            snapshot: Snapshot {
                accounts: vec![Account {
                    id: "work".into(),
                    name: "工作账号".into(),
                    active: true,
                    available: true,
                    identity_known: true,
                    windows: vec![QuotaWindow::default(); 2],
                    ..Default::default()
                }],
                ..Default::default()
            },
            ..Default::default()
        }
    }

    #[test]
    fn refresh_and_toasts_never_shift_the_account_list() {
        let model = signal(Model {
            view: populated(),
            ..Default::default()
        });
        let (tx, rx) = std::sync::mpsc::channel();
        let mut tree = Tree::new();
        let root = panel_content(model, tx.clone()).build(&mut tree);
        tree.root = Some(root);
        let mut text = NullTextEngine;
        let size = Size::new(440, 168);
        tree.layout_root(size, &mut text);
        let bounds = |tree: &Tree| {
            tree.get(root)
                .unwrap()
                .children
                .iter()
                .map(|id| tree.get(*id).unwrap().bounds)
                .collect::<Vec<_>>()
        };
        let idle = bounds(&tree);
        send(&tx, Action::Refresh, model);
        send(&tx, Action::Refresh, model);
        assert_eq!(rx.try_recv().unwrap(), Action::Refresh);
        assert!(rx.try_recv().is_err());
        tree.layout_root(size, &mut text);
        assert_eq!(bounds(&tree), idle);
        assert!(model.get().refresh_label().contains("刷新中"));
        assert_eq!(model.get().refresh_label(), "刷新中");
        for (index, error) in [false, true].into_iter().enumerate() {
            let mut view = populated();
            view.feedback_id = index as u64 + 1;
            view.feedback = Some("刷新结果".into());
            view.feedback_error = error;
            model.update(|m| m.apply(view, true));
            tree.layout_root(size, &mut text);
            assert_eq!(bounds(&tree), idle);
            let toasts = tree.take_pending_toasts();
            assert_eq!(toasts.len(), 1);
            assert_eq!(toasts[0].text, "刷新结果");
            assert_eq!(
                toasts[0].kind,
                if error {
                    windui::event::ToastKind::Error
                } else {
                    windui::event::ToastKind::Success
                }
            );
            tree.layout_root(size, &mut text);
            assert!(tree.take_pending_toasts().is_empty());
        }
    }

    #[test]
    fn spinner_rotates_each_frame_without_changing_its_footprint() {
        let bounds = Rect::new(300, 10, 88, 28);
        let mut previous = RefreshButton::arc(bounds, 0);
        for ms in (16..900).step_by(16) {
            let points = RefreshButton::arc(bounds, ms);
            assert_ne!(points, previous, "successive display frames must differ");
            for (x, y) in points {
                assert!((309.0..=319.0).contains(&x));
                assert!((19.0..=29.0).contains(&y));
            }
            previous = points;
        }
        assert_eq!(
            RefreshButton::arc(bounds, 0),
            RefreshButton::arc(bounds, 900)
        );
    }

    #[test]
    fn refresh_button_preserves_native_input_and_busy_gate() {
        use windui::event::{KeyEvent, MouseButton, PointerEvent, PointerKind};
        let model = signal(Model {
            view: populated(),
            ..Default::default()
        });
        let (tx, rx) = std::sync::mpsc::channel();
        let mut tree = Tree::new();
        let root = Element::leaf()
            .widget(RefreshButton::new(model))
            .size(88, 28)
            .enabled_when(move || !model.with(|m| m.view.busy || m.view.loading))
            .on_click(move |_| send(&tx, Action::Refresh, model))
            .build(&mut tree);
        tree.root = Some(root);
        tree.layout_root(Size::new(88, 28), &mut NullTextEngine);
        assert!(tree.focusable_order().contains(&root));
        let (mut hover, mut capture) = (None, None);
        for kind in [PointerKind::Down, PointerKind::Up] {
            tree.dispatch_pointer(
                PointerEvent::single(kind, Point::new(44, 14), MouseButton::Left),
                &mut hover,
                &mut capture,
            );
        }
        assert_eq!(rx.try_recv().unwrap(), Action::Refresh);
        tree.layout_root(Size::new(88, 28), &mut NullTextEngine);
        assert!(!tree.focusable_order().contains(&root));
        let key = KeyEvent::pressed(Key::Enter);
        tree.dispatch_key(key, Some(root));
        assert!(rx.try_recv().is_err());
        model.update(|m| m.view.busy = false);
        tree.layout_root(Size::new(88, 28), &mut NullTextEngine);
        tree.dispatch_key(key, Some(root));
        assert_eq!(rx.try_recv().unwrap(), Action::Refresh);
    }

    #[test]
    fn hidden_results_and_expired_notices_do_not_replay_on_open() {
        let mut model = Model::default();
        let view = View {
            feedback_id: 1,
            feedback: Some("隐藏时刷新完成".into()),
            ..Default::default()
        };
        model.apply(view.clone(), false);
        model.apply(view, true);
        assert!(model.feedback.is_none());
        let model = signal(model);
        let (tx, _rx) = std::sync::mpsc::channel();
        let mut tree = Tree::new();
        let root = panel_content(model, tx).build(&mut tree);
        tree.root = Some(root);
        model.update(|m| {
            m.feedback("过时的提示", true);
            m.feedback.as_mut().unwrap().created = Instant::now() - Duration::from_secs(6);
        });
        tree.layout_root(Size::new(440, 268), &mut NullTextEngine);
        assert!(tree.take_pending_toasts().is_empty());
        assert!(model.get().feedback.is_none());
    }

    #[test]
    fn multiple_favorites_sort_first_and_preserve_unavailable_accounts() {
        let mut view = populated();
        view.snapshot.accounts.extend([
            Account {
                id: "old".into(),
                ..Default::default()
            },
            Account {
                id: "other".into(),
                ..Default::default()
            },
        ]);
        view.favorites = vec!["old".into(), "work".into(), "missing".into()];
        let model = Model {
            view,
            ..Default::default()
        };
        let rows = model.rows();
        assert_eq!(rows.len(), 3);
        assert_eq!(rows.iter().filter(|r| r.pinned).count(), 2);
        assert_eq!(rows[0].account.id, "work");
        assert_eq!(rows[1].account.id, "old");
    }

    #[test]
    fn favorite_waits_for_persistence_result_and_navigation_is_available_during_refresh() {
        let model = signal(Model {
            view: populated(),
            ..Default::default()
        });
        let (tx, rx) = std::sync::mpsc::channel();
        send(&tx, Action::Favorite("work".into(), true), model);
        assert!(model.get().view.favorites.is_empty());
        assert_eq!(rx.recv().unwrap(), Action::Favorite("work".into(), true));
        send(&tx, Action::Refresh, model);
        send(&tx, Action::Hide, model);
        assert_eq!(rx.recv().unwrap(), Action::Refresh);
        assert_eq!(rx.recv().unwrap(), Action::Hide);
    }
    /// Uses the same native composition and renderer without starting a desktop.
    #[test]
    #[ignore = "writes native UI fixtures for visual inspection"]
    fn render_native_panel_fixtures() {
        let directory =
            std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("target/native-panel-fixtures");
        std::fs::create_dir_all(&directory).unwrap();
        for (name, offline, busy) in [
            ("accounts", false, false),
            ("offline", true, false),
            ("refreshing", false, true),
        ] {
            let mut view = populated();
            view.connected = !offline;
            view.busy = busy;
            view.favorites = vec!["work".into(), "personal".into()];
            view.snapshot.accounts[0].provider = "claude".into();
            view.snapshot.accounts[0].windows = vec![
                QuotaWindow {
                    name: "5h".into(),
                    percent: 38.0,
                    ..Default::default()
                },
                QuotaWindow {
                    name: "7d".into(),
                    percent: 82.0,
                    ..Default::default()
                },
                QuotaWindow {
                    name: "Fable".into(),
                    percent: 12.0,
                    ..Default::default()
                },
            ];
            view.snapshot.accounts.push(Account {
                id: "personal".into(),
                name: "个人账号".into(),
                provider: "codex".into(),
                active: true,
                available: true,
                identity_known: true,
                windows: vec![
                    QuotaWindow {
                        name: "5h".into(),
                        percent: 92.0,
                        ..Default::default()
                    },
                    QuotaWindow {
                        name: "7d".into(),
                        percent: 54.0,
                        ..Default::default()
                    },
                ],
                ..Default::default()
            });
            let model = signal(Model {
                view,
                ..Default::default()
            });
            let (tx, _rx) = std::sync::mpsc::channel();
            let path = directory.join(format!("{name}.png"));
            App::new("Native usage fixture", 440, 360)
                .content(panel_content(model, tx))
                .screenshot(&path)
                .run();
            assert!(path.is_file());
        }
    }
    #[test]
    fn close_discards_pending_feedback_before_a_rapid_reopen() {
        let mut model = Model::default();
        let view = View {
            feedback_id: 1,
            feedback: Some("刷新完成".into()),
            ..Default::default()
        };
        model.apply(view.clone(), true);
        assert!(model.feedback.is_some());
        model.clear_feedback();
        model.apply(view, true);
        assert!(model.feedback.is_none());
    }

    /// Real channel-driven lifecycle, without an app-host timer draining effects.
    #[test]
    #[ignore = "opens and closes a native tool window; run alone on an interactive desktop"]
    fn native_hover_runtime() {
        use std::ffi::c_void;
        #[link(name = "user32")]
        extern "system" {
            fn GetForegroundWindow() -> *mut c_void;
            fn FindWindowW(class: *const u16, title: *const u16) -> *mut c_void;
            fn GetWindowLongPtrW(hwnd: *mut c_void, index: i32) -> isize;
            fn IsWindowVisible(hwnd: *mut c_void) -> i32;
        }
        enum Control {
            Open,
            Close,
            Quit,
        }
        let model = signal(Model {
            view: populated(),
            ..Default::default()
        });
        let (actions, _rx) = std::sync::mpsc::channel();
        let mut app = App::resident("Native hover runtime test");
        let quit = app.tray_handle();
        let control = app.channel(move |ctx, command| match command {
            Control::Open => {
                let mut request = panel(model, actions.clone());
                // Neither app nor window timers may mask channel-effect draining.
                request.intervals.clear();
                ctx.open_window(request);
            }
            Control::Close => ctx.close_window("desktop-usage-native"),
            Control::Quit => quit.quit(),
        });
        let commands = control.clone();
        let baseline = unsafe { GetForegroundWindow() } as usize;
        let controller = std::thread::spawn(move || {
            let title: Vec<u16> = "CC Remote Desktop · 订阅用量"
                .encode_utf16()
                .chain(Some(0))
                .collect();
            let find = || unsafe { FindWindowW(std::ptr::null(), title.as_ptr()) };
            let wait = |open: bool| -> Result<(), &'static str> {
                let start = Instant::now();
                loop {
                    let hwnd = find();
                    if if open {
                        !hwnd.is_null() && unsafe { IsWindowVisible(hwnd) } != 0
                    } else {
                        hwnd.is_null()
                    } {
                        return Ok(());
                    }
                    if start.elapsed() > Duration::from_secs(3) {
                        return Err(if open {
                            "native channel open timed out"
                        } else {
                            "native channel close timed out"
                        });
                    }
                    std::thread::sleep(Duration::from_millis(50));
                }
            };
            let result = (|| -> Result<(), &'static str> {
                for cycle in 0..2 {
                    commands
                        .send(Control::Open)
                        .map_err(|_| "native channel disconnected")?;
                    wait(true)?;
                    let hwnd = find();
                    let style = unsafe { GetWindowLongPtrW(hwnd, -20) };
                    if style & 0x80 == 0 || style & 0x40000 != 0 {
                        return Err("panel is not a taskbar-free tool window");
                    }
                    let foreground = unsafe { GetForegroundWindow() } as usize;
                    if foreground != baseline {
                        eprintln!("foreground changed cycle={cycle}: before={baseline} after={foreground} popup={}",hwnd as usize);
                        return Err("hover stole foreground focus");
                    }
                    commands
                        .send(Control::Close)
                        .map_err(|_| "native channel disconnected")?;
                    wait(false)?;
                }
                Ok(())
            })();
            let _ = commands.send(Control::Quit);
            result
        });
        let ran = app.run_resident();
        let result = controller.join().expect("native runtime controller");
        // Keep the external host entrypoint alive until its message loop exits.
        drop(control);
        assert!(ran);
        assert!(result.is_ok(), "{:?}", result);
    }
}
