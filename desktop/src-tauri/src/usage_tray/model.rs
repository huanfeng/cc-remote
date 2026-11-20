use serde::{Deserialize, Serialize};

#[derive(Clone, Debug, Default, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct QuotaWindow {
    pub name: String,
    pub percent: f64,
    pub window_minutes: Option<i64>,
    pub resets_at: Option<String>,
}

#[derive(Clone, Debug, Default, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct Account {
    pub id: String,
    pub provider: String,
    pub name: String,
    #[serde(default)]
    pub active: bool,
    #[serde(default)]
    pub available: bool,
    #[serde(default)]
    pub identity_known: bool,
    pub reason: Option<String>,
    pub plan: Option<String>,
    #[serde(default)]
    pub windows: Vec<QuotaWindow>,
    pub fetched_at: Option<String>,
    #[serde(default)]
    pub stale: bool,
    pub error: Option<String>,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct Snapshot {
    pub interval_minutes: u32,
    pub accounts: Vec<Account>,
}
impl Default for Snapshot {
    fn default() -> Self {
        Self {
            interval_minutes: 10,
            accounts: vec![],
        }
    }
}

pub fn provider_label(provider: &str) -> &str {
    match provider {
        "claude" => "Claude",
        "codex" => "Codex",
        other => other,
    }
}

pub fn account_name(account: &Account) -> String {
    let provider = provider_label(&account.provider);
    let name = account.name.trim();
    let prefixed = name.get(..provider.len()).is_some_and(|prefix| {
        prefix.eq_ignore_ascii_case(provider)
            && name[provider.len()..].trim_start().starts_with('·')
    });
    if name.eq_ignore_ascii_case(provider) || prefixed {
        name.into()
    } else {
        format!("{provider} · {name}")
    }
}

#[cfg(test)]
mod name_tests {
    use super::*;

    #[test]
    fn account_names_show_provider_once_and_preserve_custom_names() {
        for (provider, name, expected) in [
            ("claude", "Claude · 本机登录", "Claude · 本机登录"),
            ("codex", "Codex · 本机登录", "Codex · 本机登录"),
            ("claude", "工作账号", "Claude · 工作账号"),
            ("claude", "Claude Team", "Claude · Claude Team"),
            ("claude", " Claude  ·  本机登录 ", "Claude  ·  本机登录"),
            ("claude", "claude·本机登录", "claude·本机登录"),
            ("codex", "Codex", "Codex"),
        ] {
            let account = Account {
                provider: provider.into(),
                name: name.into(),
                ..Default::default()
            };
            assert_eq!(account_name(&account), expected);
        }
    }

    #[test]
    fn fetch_age_and_identity_limit_remain_clear() {
        let mut account = Account {
            provider: "claude".into(),
            active: true,
            available: true,
            windows: vec![QuotaWindow::default()],
            fetched_at: Some("2026-11-20T00:00:00Z".into()),
            ..Default::default()
        };
        let at = timestamp_seconds("2026-11-20T00:00:00Z").unwrap();
        assert_eq!(updated_at(&account, at + 120), "2 分钟前获取");
        account.stale = true;
        assert_eq!(updated_at(&account, at + 7200), "2 小时前获取");
        account.stale = false;
        assert_eq!(account_status(&account), "正常采集");
        account.provider = "codex".into();
        assert_eq!(account_status(&account), "仅显示当前额度");
        account.fetched_at = None;
        assert_eq!(updated_at(&account, at), "获取时间未知");
    }
}

pub fn window_label(window: &QuotaWindow) -> String {
    let duration = match window.window_minutes.filter(|m| *m > 0) {
        Some(m) if m % 1440 == 0 => format!("{} 天", m / 1440),
        Some(m) if m % 60 == 0 => format!("{} 小时", m / 60),
        Some(m) => format!("{m} 分钟"),
        None => match window.name.as_str() {
            "5h" => "5 小时".into(),
            "7d" => "7 天".into(),
            _ => String::new(),
        },
    };
    match window.name.as_str() {
        "5h" | "7d" | "primary" | "secondary" if !duration.is_empty() => duration,
        "primary" => "主要额度".into(),
        "secondary" => "次要额度".into(),
        "" if duration.is_empty() => "订阅额度".into(),
        "" => duration,
        name if duration.is_empty() => name.into(),
        name => format!("{name} · {duration}"),
    }
}

/// Identity colors distinguish quota types; utilization keeps its separate
/// blue/amber/red scale. Named model quotas take precedence over duration.
pub fn quota_label_color(window: &QuotaWindow) -> u32 {
    match window.name.as_str() {
        "5h" => 0x2563a6,
        "7d" => 0x087c70,
        "primary" | "secondary" | "" => {
            if window.window_minutes.is_some_and(|m| m >= 1440) {
                0x087c70
            } else {
                0x2563a6
            }
        }
        _ => 0x8451b8,
    }
}

/// Invalid readings remain unknown rather than painting an empty (0%) bar.
pub fn quota_percent(window: &QuotaWindow) -> Option<f64> {
    window
        .percent
        .is_finite()
        .then(|| window.percent.clamp(0.0, 100.0))
}

#[cfg(windows)]
pub fn percent_text(window: &QuotaWindow) -> String {
    quota_percent(window)
        .map(|p| format!("已用 {p:.0}%"))
        .unwrap_or_else(|| "已用 —".into())
}

pub fn account_status(account: &Account) -> String {
    if !account.active {
        return "已停止采集 · 历史保留".into();
    }
    if account.stale || account.error.is_some() {
        return if account.windows.is_empty() {
            "暂时无法读取"
        } else {
            "数据过期 · 显示上次记录"
        }
        .into();
    }
    if !account.available {
        return match account.reason.as_deref() {
            Some("no-login") => "尚未登录".into(),
            Some("api-key") => "API 密钥不提供订阅额度".into(),
            Some("third-party") => "第三方服务不提供官方订阅额度".into(),
            _ => "订阅额度暂不可用".into(),
        };
    }
    if account.windows.is_empty() {
        return "等待首次采样".into();
    }
    if !account.identity_known && account.provider == "codex" {
        return "仅显示当前额度".into();
    }
    "正常采集".into()
}

fn now_seconds() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs().min(i64::MAX as u64) as i64)
        .unwrap_or(0)
}

// Backend timestamps are RFC3339. Parse the UTC instant, retaining zone offsets
// and fractional seconds without introducing a date/time runtime dependency.
fn timestamp_seconds(value: &str) -> Option<i64> {
    let b = value.as_bytes();
    if b.len() < 20
        || b[4] != b'-'
        || b[7] != b'-'
        || !matches!(b[10], b'T' | b't')
        || b[13] != b':'
        || b[16] != b':'
    {
        return None;
    }
    let number = |range: std::ops::Range<usize>| -> Option<i64> {
        let digits = b.get(range)?;
        if !digits.iter().all(u8::is_ascii_digit) {
            return None;
        }
        digits.iter().try_fold(0i64, |v, d| {
            v.checked_mul(10)?.checked_add((d - b'0') as i64)
        })
    };
    let year = number(0..4)?;
    let month = number(5..7)?;
    let day = number(8..10)?;
    let hour = number(11..13)?;
    let minute = number(14..16)?;
    let second = number(17..19)?;
    let leap = year % 4 == 0 && (year % 100 != 0 || year % 400 == 0);
    let month_days = [
        31,
        if leap { 29 } else { 28 },
        31,
        30,
        31,
        30,
        31,
        31,
        30,
        31,
        30,
        31,
    ];
    if !(1..=12).contains(&month)
        || day < 1
        || day > month_days[(month - 1) as usize]
        || hour > 23
        || minute > 59
        || second > 59
    {
        return None;
    }
    let mut zone = 19;
    if b.get(zone) == Some(&b'.') {
        zone += 1;
        let start = zone;
        while b.get(zone).is_some_and(u8::is_ascii_digit) {
            zone += 1;
        }
        if zone == start {
            return None;
        }
    }
    let offset = match b.get(zone)? {
        b'Z' | b'z' if zone + 1 == b.len() => 0,
        sign @ (b'+' | b'-') if zone + 6 == b.len() && b[zone + 3] == b':' => {
            let hours = number(zone + 1..zone + 3)?;
            let minutes = number(zone + 4..zone + 6)?;
            if hours > 23 || minutes > 59 {
                return None;
            }
            (hours * 60 + minutes) * 60 * if *sign == b'+' { 1 } else { -1 }
        }
        _ => return None,
    };
    let y = year - i64::from(month <= 2);
    let era = y.div_euclid(400);
    let yo = y - era * 400;
    let mp = month + if month > 2 { -3 } else { 9 };
    let doy = (153 * mp + 2) / 5 + day - 1;
    let days = era * 146097 + yo * 365 + yo / 4 - yo / 100 + doy - 719468;
    Some(days * 86400 + hour * 3600 + minute * 60 + second - offset)
}

fn updated_at(account: &Account, now: i64) -> String {
    let Some(at) = account.fetched_at.as_deref().and_then(timestamp_seconds) else {
        return "获取时间未知".into();
    };
    let elapsed = now.saturating_sub(at).max(0);
    match elapsed {
        0..=59 => "刚刚获取".into(),
        60..=3599 => format!("{} 分钟前获取", elapsed / 60),
        3600..=86399 => format!("{} 小时前获取", elapsed / 3600),
        _ => format!("{} 天前获取", elapsed / 86400),
    }
}
pub fn updated_text(account: &Account) -> String {
    updated_at(account, now_seconds())
}

fn resets_at(window: &QuotaWindow, now: i64) -> String {
    let Some(at) = window.resets_at.as_deref().and_then(timestamp_seconds) else {
        return "重置时间未知".into();
    };
    let remaining = at.saturating_sub(now);
    if remaining <= 0 {
        return "等待额度重置".into();
    }
    let minutes = (remaining + 59) / 60;
    if minutes < 60 {
        return format!("{minutes} 分钟后重置");
    }
    let hours = minutes / 60;
    if hours < 24 {
        return if minutes % 60 == 0 {
            format!("{hours} 小时后重置")
        } else {
            format!("{hours} 小时 {} 分后重置", minutes % 60)
        };
    }
    let days = hours / 24;
    if hours % 24 == 0 {
        format!("{days} 天后重置")
    } else {
        format!("{days} 天 {} 小时后重置", hours % 24)
    }
}
pub fn reset_text(window: &QuotaWindow) -> String {
    resets_at(window, now_seconds())
}
