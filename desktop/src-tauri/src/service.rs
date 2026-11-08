// The cc-remote service installed on this computer, through its own CLI: its
// state (`service status --json`), start / stop / restart, and its access token
// so the app can add it as a host without the user copying it.
use std::path::PathBuf;
use std::process::Stdio;
use std::time::Duration;

use serde_json::{json, Value};

/// Where `cc-remote service install` puts its copy (internal/service/config.go InstallRoot).
fn install_root() -> Option<PathBuf> {
    let home = || std::env::var_os(if cfg!(windows) { "USERPROFILE" } else { "HOME" }).map(PathBuf::from);
    if cfg!(windows) {
        std::env::var_os("LOCALAPPDATA")
            .map(PathBuf::from)
            .filter(|p| p.is_absolute())
            .or_else(|| home().map(|h| h.join("AppData").join("Local")))
            .map(|p| p.join("cc-remote"))
    } else if cfg!(target_os = "macos") {
        home().map(|h| h.join("Library").join("Application Support").join("cc-remote"))
    } else {
        std::env::var_os("XDG_DATA_HOME")
            .map(PathBuf::from)
            .filter(|p| p.is_absolute())
            .or_else(|| home().map(|h| h.join(".local").join("share")))
            .map(|p| p.join("cc-remote"))
    }
}

/// The installed copy, else a cc-remote on PATH.
fn executable() -> Option<PathBuf> {
    let name = if cfg!(windows) { "cc-remote.exe" } else { "cc-remote" };
    if let Some(path) = install_root().map(|r| r.join("bin").join(name)).filter(|p| p.is_file()) {
        return Some(path);
    }
    let paths = std::env::var_os("PATH")?;
    std::env::split_paths(&paths).map(|dir| dir.join(name)).find(|p| p.is_file())
}

struct Output {
    code: Option<i32>,
    stdout: String,
    stderr: String,
}

async fn cc_remote(exe: &PathBuf, args: &[&str], lang: &str, timeout: Duration) -> Result<Output, String> {
    let mut command = tokio::process::Command::new(exe);
    command
        .args(args)
        .env("CC_REMOTE_LANG", lang)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .kill_on_drop(true);
    #[cfg(windows)]
    command.creation_flags(0x0800_0000); // CREATE_NO_WINDOW: no console flashing up
    let child = command.spawn().map_err(|e| e.to_string())?;
    let output = tokio::time::timeout(timeout, child.wait_with_output())
        .await
        .map_err(|_| format!("cc-remote {} timed out", args.join(" ")))?
        .map_err(|e| e.to_string())?;
    Ok(Output {
        code: output.status.code(),
        stdout: String::from_utf8_lossy(&output.stdout).into_owned(),
        stderr: String::from_utf8_lossy(&output.stderr).into_owned(),
    })
}

async fn status(exe: &PathBuf, lang: &str) -> Result<Value, String> {
    let out = cc_remote(exe, &["service", "status", "--json"], lang, Duration::from_secs(20)).await?;
    // Exit 3 is "not running", still with the JSON.
    serde_json::from_str::<Value>(&out.stdout).map_err(|_| {
        let text = if out.stderr.trim().is_empty() { out.stdout } else { out.stderr };
        format!("cc-remote service status: {}", text.trim())
    })
}

/// The local service: { available: false } when cc-remote is not installed,
/// else its status JSON plus `exe`. The token is left out (see local_service_token).
#[tauri::command]
pub async fn local_service_status(lang: String) -> Result<Value, String> {
    let Some(exe) = executable() else {
        return Ok(json!({ "available": false }));
    };
    let mut value = status(&exe, &lang).await?;
    if let Value::Object(map) = &mut value {
        map.insert("available".into(), Value::Bool(true));
        map.insert("exe".into(), Value::String(exe.to_string_lossy().into_owned()));
    }
    Ok(value)
}

/// The local host's access token, read from the data directory its service uses.
#[tauri::command]
pub async fn local_service_token(lang: String) -> Result<String, String> {
    let exe = executable().ok_or("cc-remote is not installed")?;
    let value = status(&exe, &lang).await?;
    let dir = value
        .pointer("/config/dataDir")
        .and_then(Value::as_str)
        .ok_or("the service has no data directory")?;
    let token = std::fs::read_to_string(PathBuf::from(dir).join("access-token")).map_err(|e| e.to_string())?;
    Ok(token.trim().to_string())
}

/// start, stop or restart; the CLI's own message on failure.
#[tauri::command]
pub async fn local_service_action(action: String, lang: String) -> Result<String, String> {
    if !matches!(action.as_str(), "start" | "stop" | "restart") {
        return Err(format!("unknown action: {action}"));
    }
    let exe = executable().ok_or("cc-remote is not installed")?;
    let out = cc_remote(&exe, &["service", &action], &lang, Duration::from_secs(90)).await?;
    if out.code == Some(0) {
        Ok(out.stdout.trim().to_string())
    } else {
        let text = if out.stderr.trim().is_empty() { out.stdout } else { out.stderr };
        Err(text.trim().to_string())
    }
}

#[cfg(test)]
mod tests {
    /// Reads the real service's state (read-only): CC_TEST_LOCAL_SERVICE=1 cargo test.
    #[test]
    fn local_status_parses() {
        if std::env::var("CC_TEST_LOCAL_SERVICE").is_err() {
            return;
        }
        let rt = tokio::runtime::Builder::new_current_thread().enable_all().build().unwrap();
        let v = rt.block_on(super::local_service_status("en".into())).unwrap();
        assert_eq!(v["available"], true, "{v}");
        assert!(v.get("running").is_some() && v.get("exe").is_some(), "{v}");
        println!("running={} url={} version={}", v["running"], v["url"], v.pointer("/config/version").unwrap_or(&serde_json::Value::Null));
        let token = rt.block_on(super::local_service_token("en".into())).unwrap();
        assert!(token.len() > 16);
    }
}
