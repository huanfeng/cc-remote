//! Explicit local file-manager and terminal actions. Remote host paths never reach the OS.
use std::path::{Path, PathBuf};
use std::process::Command;

#[derive(Clone, Copy, serde::Deserialize, Debug, PartialEq)]
#[serde(rename_all = "lowercase")]
pub enum PathAction {
    Open,
    #[serde(rename = "open-file")]
    OpenFile,
    Reveal,
    Terminal,
}

fn validate_host(value: &str) -> Result<(), String> {
    if value.chars().any(char::is_control) {
        return Err("主机地址包含控制字符".into());
    }
    let url = reqwest::Url::parse(value).map_err(|_| "主机地址无效")?;
    let local = url.host_str().is_some_and(|host| {
        host.eq_ignore_ascii_case("localhost")
            || host
                .trim_matches(['[', ']'])
                .parse::<std::net::IpAddr>()
                .is_ok_and(|ip| ip.is_loopback())
    });
    if !matches!(url.scheme(), "http" | "https")
        || !local
        || !url.username().is_empty()
        || url.password().is_some()
    {
        return Err("仅本机回环地址的项目支持系统路径操作".into());
    }
    Ok(())
}

fn validate_path_text(value: &str) -> Result<(), String> {
    if value.is_empty()
        || value.chars().any(char::is_control)
        || value.starts_with("//")
        || value.starts_with('\\')
    {
        return Err("仅支持不含控制字符的本机绝对路径，不支持网络或设备路径".into());
    }
    if !Path::new(value).is_absolute() {
        return Err("需要本机绝对路径".into());
    }
    #[cfg(windows)]
    {
        if value.get(2..).is_some_and(|tail| tail.contains(':')) {
            return Err("不支持文件流路径".into());
        }
        local_drive(Path::new(value))?;
    }
    Ok(())
}

#[cfg(windows)]
fn local_drive(path: &Path) -> Result<(), String> {
    use std::path::{Component, Prefix};
    let letter = match path.components().next() {
        Some(Component::Prefix(prefix)) => match prefix.kind() {
            Prefix::Disk(letter) | Prefix::VerbatimDisk(letter) => letter,
            _ => return Err("不支持网络或设备路径".into()),
        },
        _ => return Err("需要本机驱动器绝对路径".into()),
    };
    #[link(name = "kernel32")]
    unsafe extern "system" {
        fn GetDriveTypeW(root: *const u16) -> u32;
    }
    let root = [letter as u16, b':' as u16, b'\\' as u16, 0];
    // Query before filesystem access too, so a mapped network drive is never traversed.
    if !matches!(unsafe { GetDriveTypeW(root.as_ptr()) }, 2 | 3 | 5 | 6) {
        return Err("仅支持本机驱动器，不支持映射的网络驱动器".into());
    }
    Ok(())
}

fn target(value: &str, action: PathAction) -> Result<PathBuf, String> {
    validate_path_text(value)?;
    let canonical =
        std::fs::canonicalize(value).map_err(|error| format!("无法访问路径：{error}"))?;
    #[cfg(windows)]
    local_drive(&canonical)?;
    let metadata = canonical
        .metadata()
        .map_err(|error| format!("无法读取路径：{error}"))?;
    if !metadata.is_dir() && !metadata.is_file() {
        return Err("仅支持普通文件或文件夹".into());
    }
    let canonical = if action == PathAction::Terminal && metadata.is_file() {
        canonical
            .parent()
            .ok_or("无法确定文件所在目录")?
            .to_path_buf()
    } else {
        canonical
    };
    if action == PathAction::Open && !metadata.is_dir() {
        return Err("打开文件夹需要目录路径；文件请使用在文件管理器中显示".into());
    }
    if action == PathAction::OpenFile && !metadata.is_file() {
        return Err("打开文件需要普通文件路径".into());
    }
    // Windows shell tools expect a normal drive path, not Rust's canonical \\?\ form.
    #[cfg(windows)]
    let canonical = {
        let text = canonical.to_str().ok_or("路径包含无法显示的字符")?;
        PathBuf::from(text.strip_prefix(r"\\?\").unwrap_or(text))
    };
    Ok(canonical)
}

#[cfg(windows)]
fn terminal_command(directory: &Path) -> Result<Command, String> {
    use std::os::windows::process::CommandExt;
    let root = std::env::var_os("SystemRoot")
        .map(PathBuf::from)
        .filter(|p| p.is_absolute())
        .ok_or("无法确定 Windows 系统目录")?;
    let mut command = Command::new(root.join("System32/WindowsPowerShell/v1.0/powershell.exe"));
    command
        .args(["-NoLogo", "-NoProfile", "-NoExit"])
        .current_dir(directory);
    // The user explicitly requested an interactive terminal window.
    command.creation_flags(0x00000010); // CREATE_NEW_CONSOLE
    Ok(command)
}

#[cfg(target_os = "macos")]
fn terminal_command(directory: &Path) -> Result<Command, String> {
    let mut command = Command::new("/usr/bin/open");
    command
        .args(["-a", "Terminal"])
        .arg(directory)
        .current_dir(directory);
    Ok(command)
}

#[cfg(not(any(windows, target_os = "macos")))]
fn terminal_command(directory: &Path) -> Result<Command, String> {
    for (program, option) in [
        ("gnome-terminal", Some("--working-directory")),
        ("konsole", Some("--workdir")),
        ("xfce4-terminal", Some("--working-directory")),
        ("x-terminal-emulator", None),
        ("xterm", None),
    ] {
        if let Some(executable) = std::env::var_os("PATH").and_then(|path| {
            std::env::split_paths(&path)
                .map(|entry| entry.join(program))
                .find(|path| path.is_absolute() && path.is_file())
        }) {
            let mut command = Command::new(executable);
            if let Some(option) = option {
                command.arg(option).arg(directory);
            }
            command.current_dir(directory);
            return Ok(command);
        }
    }
    Err("未找到终端程序，请安装系统终端后重试".into())
}

fn perform(host_url: &str, path: &str, action: PathAction) -> Result<(), String> {
    validate_host(host_url)?;
    let path = target(path, action)?;
    match action {
        PathAction::Open => tauri_plugin_opener::open_path(path, None::<&str>)
            .map_err(|error| format!("无法打开文件夹：{error}")),
        PathAction::OpenFile => tauri_plugin_opener::open_path(path, None::<&str>)
            .map_err(|error| format!("无法打开文件：{error}")),
        PathAction::Reveal => tauri_plugin_opener::reveal_item_in_dir(path)
            .map_err(|error| format!("无法在文件管理器中显示：{error}")),
        PathAction::Terminal => terminal_command(&path)?
            .spawn()
            .map(|_| ())
            .map_err(|error| format!("无法打开终端：{error}")),
    }
}

#[tauri::command]
pub async fn open_local_path(
    host_url: String,
    path: String,
    action: PathAction,
) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || perform(&host_url, &path, action))
        .await
        .map_err(|error| format!("系统路径操作未完成：{error}"))?
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn only_http_loopback_hosts_are_allowed() {
        for host in [
            "http://localhost:8787",
            "https://127.0.0.1",
            "http://[::1]:8787",
            "http://127.3.2.1",
        ] {
            assert!(validate_host(host).is_ok(), "{host}");
        }
        for host in [
            "",
            "file:///tmp",
            "https://localhost.example",
            "http://192.168.1.2",
            "http://0.0.0.0",
            "http://user@localhost",
            "http://localhost\n",
        ] {
            assert!(validate_host(host).is_err(), "{host}");
        }
    }
    #[test]
    fn reject_relative_network_device_and_control_paths() {
        for path in [
            "relative",
            "../folder",
            "//server/share",
            r"\\server\share",
            r"\\?\C:\folder",
            r"\\.\pipe\test",
            "bad\npath",
        ] {
            assert!(validate_path_text(path).is_err(), "{path}");
        }
        #[cfg(windows)]
        for path in [r"C:relative", r"C:\file:stream"] {
            assert!(validate_path_text(path).is_err(), "{path}");
        }
    }
    #[cfg(windows)]
    #[test]
    fn terminal_uses_literal_working_directory_and_fixed_arguments() {
        let directory = Path::new(r"C:\空 格 & '$folder");
        let command = terminal_command(directory).unwrap();
        assert_eq!(command.get_current_dir(), Some(directory));
        assert_eq!(
            command.get_args().collect::<Vec<_>>(),
            ["-NoLogo", "-NoProfile", "-NoExit"]
        );
        assert!(command
            .get_program()
            .to_string_lossy()
            .ends_with("powershell.exe"));
    }

    #[test]
    fn local_path_actions_validate_file_and_directory_targets() {
        let directory =
            std::env::temp_dir().join(format!("cc-path-action-{}-空 格 & '$", std::process::id()));
        std::fs::create_dir_all(&directory).unwrap();
        let file = directory.join("空 格 & '$.txt");
        std::fs::write(&file, "data").unwrap();
        let value = file.to_str().unwrap();
        assert!(target(value, PathAction::Open).is_err());
        assert!(target(value, PathAction::OpenFile).unwrap().is_file());
        assert!(target(directory.to_str().unwrap(), PathAction::OpenFile).is_err());
        assert!(perform("https://remote.example", value, PathAction::OpenFile).is_err());
        assert!(target(value, PathAction::Reveal).unwrap().is_file());
        assert!(target(value, PathAction::Terminal).unwrap().is_dir());
        assert!(target(
            directory.join("missing").to_str().unwrap(),
            PathAction::Reveal
        )
        .is_err());
        let resolved = target(value, PathAction::Terminal).unwrap();
        if let Ok(command) = terminal_command(&resolved) {
            assert_eq!(command.get_current_dir(), Some(resolved.as_path()));
            #[cfg(windows)]
            assert_eq!(
                command.get_args().collect::<Vec<_>>(),
                ["-NoLogo", "-NoProfile", "-NoExit"]
            );
        }
        std::fs::remove_file(file).unwrap();
        std::fs::remove_dir(directory).unwrap();
    }
}
