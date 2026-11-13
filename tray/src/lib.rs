use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

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

#[derive(Clone, Default)]
pub struct Options {
    pub host: Option<String>,
    pub data_dir: Option<PathBuf>,
    pub cc_remote: Option<PathBuf>,
    pub desktop: Option<PathBuf>,
    pub check: bool,
    pub smoke_seconds: Option<u64>,
}
impl Options {
    pub fn parse(args: impl IntoIterator<Item = String>) -> Result<Self, String> {
        let mut o = Self::default();
        let mut args = args.into_iter();
        while let Some(arg) = args.next() {
            let mut value = || {
                args.next()
                    .ok_or_else(|| format!("Missing value for {arg}"))
            };
            match arg.as_str() {
                "--host" => o.host = Some(local_origin(&value()?)?),
                "--data-dir" => o.data_dir = Some(PathBuf::from(value()?)),
                "--cc-remote" => o.cc_remote = Some(PathBuf::from(value()?)),
                "--desktop" => o.desktop = Some(PathBuf::from(value()?)),
                "--check" => o.check = true,
                "--smoke-seconds" => {
                    o.smoke_seconds = Some(value()?.parse().map_err(|_| "Invalid smoke duration")?)
                }
                // windui consumes these arguments itself.
                "--screenshot" => {
                    let _ = value()?;
                }
                "--scale" => {
                    let _ = value()?;
                }
                _ => return Err(format!("Unknown argument: {arg}")),
            }
        }
        if o.host.is_some() != o.data_dir.is_some() {
            return Err("Use --host and --data-dir together".into());
        }
        if o.data_dir.as_ref().is_some_and(|p| !p.is_absolute()) {
            return Err("--data-dir must be absolute".into());
        }
        Ok(o)
    }
}

pub fn local_origin(value: &str) -> Result<String, String> {
    let url = reqwest::Url::parse(value).map_err(|_| "Invalid host address")?;
    let hostname = url.host_str().unwrap_or("").trim_matches(['[', ']']);
    let local = hostname.eq_ignore_ascii_case("localhost")
        || hostname
            .parse::<std::net::IpAddr>()
            .is_ok_and(|p| p.is_loopback());
    if !local
        || !matches!(url.scheme(), "http" | "https")
        || !url.username().is_empty()
        || url.password().is_some()
        || url.query().is_some()
        || url.fragment().is_some()
        || url.path() != "/"
    {
        return Err("The tray requires a loopback HTTP(S) origin".into());
    }
    Ok(url.as_str().trim_end_matches('/').to_string())
}

fn binary(name: &str) -> String {
    format!("{name}{}", if cfg!(windows) { ".exe" } else { "" })
}
pub fn find_binary(name: &str, explicit: Option<&Path>) -> Option<PathBuf> {
    if let Some(path) = explicit {
        return path.is_file().then(|| path.to_path_buf());
    }
    let filename = binary(name);
    let mut paths = vec![];
    if let Ok(exe) = std::env::current_exe() {
        if let Some(dir) = exe.parent() {
            paths.push(dir.join(&filename));
        }
    }
    let root = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .parent()?
        .to_path_buf();
    if name == "cc-remote-desktop" {
        paths.push(
            root.join("desktop/src-tauri/target/release")
                .join(&filename),
        );
    }
    paths.push(root.join(&filename));
    if let Some(local) = std::env::var_os("LOCALAPPDATA") {
        paths.push(PathBuf::from(&local).join("cc-remote/bin").join(&filename));
        paths.push(PathBuf::from(local).join("CC Remote").join(&filename));
    }
    if let Some(home) = std::env::var_os("HOME") {
        paths.push(
            PathBuf::from(&home)
                .join(".local/share/cc-remote/bin")
                .join(&filename),
        );
        paths.push(
            PathBuf::from(home)
                .join("Library/Application Support/cc-remote/bin")
                .join(&filename),
        );
    }
    if let Some(path) = std::env::var_os("PATH") {
        paths.extend(std::env::split_paths(&path).map(|p| p.join(&filename)));
    }
    paths.into_iter().find(|p| p.is_file())
}
pub fn hidden_command(path: &Path) -> Command {
    let mut cmd = Command::new(path);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        cmd.creation_flags(0x0800_0000);
    }
    cmd.stdin(Stdio::null());
    cmd
}

#[derive(Clone, Debug)]
pub struct Connection {
    pub host: String,
    pub data_dir: PathBuf,
}
pub fn discover(o: &Options) -> Result<Connection, String> {
    if let (Some(host), Some(dir)) = (&o.host, &o.data_dir) {
        return Ok(Connection {
            host: local_origin(host)?,
            data_dir: dir.clone(),
        });
    }
    let exe = find_binary("cc-remote", o.cc_remote.as_deref())
        .ok_or("找不到 cc-remote，请先安装后台，或指定 --cc-remote")?;
    let mut child = hidden_command(&exe)
        .args(["service", "status", "--json"])
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .map_err(|_| "无法读取本机服务状态")?;
    let deadline = Instant::now() + Duration::from_secs(15);
    loop {
        if child.try_wait().map_err(|_| "无法检查服务状态")?.is_some() {
            break;
        }
        if Instant::now() >= deadline {
            let _ = child.kill();
            let _ = child.wait();
            return Err("读取服务状态超时".into());
        }
        std::thread::sleep(Duration::from_millis(100));
    }
    let out = child.wait_with_output().map_err(|_| "无法读取服务状态")?;
    let data: serde_json::Value =
        serde_json::from_slice(&out.stdout).map_err(|_| "服务状态格式不正确")?;
    let host = data["url"].as_str().ok_or("cc-remote 后台未运行")?;
    let dir = data
        .pointer("/config/dataDir")
        .and_then(|v| v.as_str())
        .ok_or("本机服务没有数据目录")?;
    let dir = PathBuf::from(dir);
    if !dir.is_absolute() {
        return Err("服务数据目录必须是绝对路径".into());
    }
    Ok(Connection {
        host: local_origin(host)?,
        data_dir: dir,
    })
}

pub struct MonitorClient {
    client: reqwest::blocking::Client,
}
impl MonitorClient {
    pub fn new() -> Result<Self, String> {
        let client = reqwest::blocking::Client::builder()
            .timeout(Duration::from_secs(120))
            .connect_timeout(Duration::from_secs(5))
            .redirect(reqwest::redirect::Policy::none())
            .no_proxy()
            .build()
            .map_err(|_| "无法创建本机连接")?;
        Ok(Self { client })
    }
    pub fn request(&self, connection: &Connection, action: Action) -> Result<Snapshot, String> {
        let host = local_origin(&connection.host)?;
        let token = std::fs::read_to_string(connection.data_dir.join("access-token"))
            .map_err(|_| "无法读取本机访问令牌")?;
        if token.trim().is_empty() {
            return Err("本机访问令牌为空".into());
        }
        let (method, path) = match action {
            Action::Read => (reqwest::Method::GET, ""),
            Action::Refresh => (reqwest::Method::POST, "/refresh"),
            Action::Interval(_) => (reqwest::Method::PUT, "/settings"),
            Action::History(_) => return Err("Invalid network action".into()),
        };
        let mut request = self
            .client
            .request(method, format!("{host}/api/usage/monitor{path}"))
            .bearer_auth(token.trim());
        if let Action::Interval(minutes) = action {
            request = request.json(&serde_json::json!({"intervalMinutes":minutes}));
        }
        let reply = request.send().map_err(|_| "无法连接 cc-remote 后台")?;
        if !reply.status().is_success() {
            return Err(match reply.status().as_u16() {
                401 => "访问令牌已变化，请检查后台数据目录",
                404 => "后台版本过旧，请更新 cc-remote",
                _ => "后台暂时无法提供用量",
            }
            .into());
        }
        reply.json().map_err(|_| "后台用量格式不正确".to_string())
    }
}
#[derive(Clone)]
pub enum Action {
    Read,
    Refresh,
    Interval(u32),
    History(Option<String>),
}

pub fn open_history(o: &Options, c: &Connection, account: Option<String>) -> Result<(), String> {
    let exe = find_binary("cc-remote-desktop", o.desktop.as_deref())
        .ok_or("找不到桌面客户端，请安装新版客户端或指定 --desktop")?;
    let mut cmd = hidden_command(&exe);
    cmd.arg("--usage-history")
        .arg("--host")
        .arg(local_origin(&c.host)?)
        .arg("--data-dir")
        .arg(&c.data_dir);
    if let Some(account) = account {
        cmd.arg("--account").arg(account);
    }
    cmd.stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .map_err(|_| "无法启动桌面客户端")?;
    Ok(())
}

pub fn account_text(a: &Account) -> String {
    let windows = a
        .windows
        .iter()
        .take(2)
        .map(|w| format!("{} {:.0}%", w.name, w.percent))
        .collect::<Vec<_>>()
        .join(" · ");
    format!(
        "{} · {}\n{}{}{}",
        a.provider,
        a.name,
        if windows.is_empty() {
            "等待采样"
        } else {
            &windows
        },
        if a.stale { " · 数据过期" } else { "" },
        if !a.active { " · 已停止采集" } else { "" }
    )
}
/// Win32 NOTIFYICONDATA has room for 127 UTF-16 units plus NUL.
pub fn tooltip(snapshot: &Snapshot, focused: &str, error: Option<&str>) -> String {
    let account = snapshot
        .accounts
        .iter()
        .find(|a| a.id == focused)
        .or_else(|| snapshot.accounts.iter().find(|a| a.active));
    let text = if let Some(a) = account {
        format!(
            "CC Remote\n{}{}",
            account_text(a),
            if error.is_some() {
                "\n连接中断 · 数据过期"
            } else {
                ""
            }
        )
    } else {
        format!("CC Remote\n{}", error.unwrap_or("等待后台采样 · 点击查看"))
    };
    let mut units = 0;
    text.chars()
        .take_while(|c| {
            units += c.len_utf16();
            units <= 127
        })
        .collect()
}

pub fn preferences_path() -> PathBuf {
    let root = std::env::var_os("LOCALAPPDATA")
        .map(PathBuf::from)
        .or_else(|| std::env::var_os("XDG_CONFIG_HOME").map(PathBuf::from))
        .or_else(|| std::env::var_os("HOME").map(|p| PathBuf::from(p).join(".config")))
        .unwrap_or_else(std::env::temp_dir);
    root.join("cc-remote-tray/preferences.json")
}
pub fn load_focus() -> String {
    std::fs::read(preferences_path())
        .ok()
        .and_then(|v| serde_json::from_slice::<serde_json::Value>(&v).ok())
        .and_then(|v| v["focusedAccount"].as_str().map(str::to_string))
        .unwrap_or_default()
}
pub fn save_focus(id: &str) -> Result<(), String> {
    let path = preferences_path();
    std::fs::create_dir_all(path.parent().unwrap()).map_err(|_| "无法保存关注账号")?;
    std::fs::write(
        path,
        serde_json::to_vec(&serde_json::json!({"focusedAccount":id})).unwrap(),
    )
    .map_err(|_| "无法保存关注账号".into())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn credentials_only_go_to_loopback_origins() {
        for host in [
            "https://example.com",
            "http://localhost.example.com",
            "http://localhost/path",
            "http://x@localhost",
            "http://localhost/?x=1",
        ] {
            assert!(local_origin(host).is_err());
        }
        assert_eq!(
            local_origin("http://127.0.0.1:8790/").unwrap(),
            "http://127.0.0.1:8790"
        );
        assert!(local_origin("http://[::1]:8790").is_ok());
    }
    #[test]
    fn cli_rejects_incomplete_custom_connection() {
        assert!(Options::parse(["--host", "http://localhost"].map(str::to_string)).is_err());
        assert!(Options::parse(["--data-dir"].map(str::to_string)).is_err());
        assert!(Options::parse(["--unknown"].map(str::to_string)).is_err());
    }
    #[test]
    fn tooltip_keeps_surrogates_intact_and_selects_account() {
        let a = Account {
            id: "a".into(),
            name: "😀账号".repeat(100),
            active: true,
            ..Default::default()
        };
        let b = Account {
            id: "b".into(),
            name: "工作".into(),
            active: true,
            ..Default::default()
        };
        let s = Snapshot {
            accounts: vec![a, b],
            ..Default::default()
        };
        assert!(tooltip(&s, "a", None).encode_utf16().count() <= 127);
        assert!(tooltip(&s, "b", Some("offline")).contains("数据过期"));
        assert!(tooltip(&s, "b", None).contains("工作"));
    }
}
