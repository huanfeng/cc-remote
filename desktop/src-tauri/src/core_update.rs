//! Updates the registered local Core through its CLI, independently of Tauri.
use std::{
    path::PathBuf,
    sync::Mutex,
    time::{Duration, Instant},
};

use serde::{Deserialize, Serialize};
use serde_json::Value;
use tauri::{AppHandle, Emitter, Manager};

use crate::{service, update::ComponentInstallLock};

#[derive(Clone, Copy, Default, PartialEq, Eq, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
enum Phase {
    #[default]
    Idle,
    Unavailable,
    Unmanaged,
    Checking,
    Available,
    Installing,
    Waiting,
    Restarting,
    Updated,
    Error,
}

impl Phase {
    fn activating(self) -> bool {
        matches!(self, Self::Waiting | Self::Restarting)
    }
}

#[derive(Clone, Default, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct View {
    revision: u64,
    current_version: Option<String>,
    installed_version: Option<String>,
    running: bool,
    phase: Phase,
    version: Option<String>,
    error: Option<String>,
}

#[derive(Default)]
pub(crate) struct CoreUpdate {
    view: Mutex<View>,
    operation: tokio::sync::Mutex<()>,
}

impl CoreUpdate {
    fn view(&self) -> View {
        self.view.lock().unwrap().clone()
    }
}

fn change(app: &AppHandle, edit: impl FnOnce(&mut View)) -> View {
    let state = app.state::<CoreUpdate>();
    let mut view = state.view.lock().unwrap();
    let old = view.clone();
    edit(&mut view);
    if *view != old {
        view.revision += 1;
        let _ = app.emit("core-update", &*view);
    }
    view.clone()
}

#[derive(Clone, Default)]
struct Local {
    installed_version: Option<String>,
    running_version: Option<String>,
    running: bool,
    pid: Option<u64>,
    restart_pending: Option<bool>,
}

impl Local {
    fn from_status(status: &Value) -> Self {
        let string = |path| {
            status
                .pointer(path)
                .and_then(Value::as_str)
                .map(str::to_owned)
        };
        Self {
            installed_version: string("/config/version"),
            running_version: string("/status/version"),
            running: status["running"].as_bool() == Some(true),
            pid: status.pointer("/status/pid").and_then(Value::as_u64),
            restart_pending: status
                .pointer("/status/restartPending")
                .and_then(Value::as_bool),
        }
    }

    fn record(&self, view: &mut View) {
        view.current_version.clone_from(&self.running_version);
        view.installed_version.clone_from(&self.installed_version);
        view.running = self.running;
    }

    fn installed(&self, target: &str) -> bool {
        self.installed_version
            .as_deref()
            .is_some_and(|version| same_version(version, target))
    }

    fn active(&self, target: &str) -> bool {
        self.running
            && self
                .running_version
                .as_deref()
                .is_some_and(|version| same_version(version, target))
    }

    fn activation(&self, target: &str, previous: &Local) -> Phase {
        if self.installed(target) && self.active(target) {
            Phase::Updated
        } else if self.installed(target)
            && self.running
            && self.running_version.is_some()
            && self.running_version == previous.running_version
            && self.pid == previous.pid
            && self.restart_pending != Some(false)
        {
            Phase::Waiting
        } else {
            Phase::Restarting
        }
    }
}

enum Discovery {
    Managed(PathBuf, Local),
    Unavailable(Phase),
}

async fn discover(lang: &str) -> Result<Discovery, String> {
    let Some(exe) = service::installed_executable() else {
        let phase = if service::executable().is_some() || service::portable_executable_exists() {
            Phase::Unmanaged
        } else {
            Phase::Unavailable
        };
        return Ok(Discovery::Unavailable(phase));
    };
    let status = service::status(&exe, lang).await?;
    if status["installed"].as_bool() != Some(true)
        || !status["config"].is_object()
        || status
            .pointer("/status/service/managed")
            .and_then(Value::as_bool)
            == Some(false)
    {
        return Ok(Discovery::Unavailable(Phase::Unmanaged));
    }
    Ok(Discovery::Managed(exe, Local::from_status(&status)))
}

fn core_error(app: &AppHandle, error: String, local: Option<&Local>) -> View {
    change(app, |view| {
        if let Some(local) = local {
            local.record(view);
            if view
                .version
                .as_deref()
                .is_some_and(|target| local.installed(target))
            {
                view.version = None;
            }
        }
        view.phase = Phase::Error;
        view.error = Some(error);
    })
}

fn unavailable(app: &AppHandle, phase: Phase) -> View {
    change(app, |view| {
        *view = View {
            revision: view.revision,
            phase,
            ..View::default()
        };
    })
}

fn same_version(a: &str, b: &str) -> bool {
    a.trim_start_matches('v') == b.trim_start_matches('v')
}

fn message<'a>(lang: &str, en: &'a str, zh: &'a str) -> &'a str {
    if lang == "zh" {
        zh
    } else {
        en
    }
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct Candidate {
    current_version: String,
    version: String,
    comparable: bool,
    available: bool,
}

// Releases before --check --json used these fixed English lines. Keep this
// compatibility path at the CLI boundary until those versions can be retired.
fn legacy_candidate(output: &str) -> Option<Candidate> {
    let mut lines = output.lines();
    let current_version = lines.next()?.strip_prefix("current version: ")?;
    let (version, _) = lines
        .next()?
        .strip_prefix("release version: ")?
        .split_once(" (via ")?;
    let (comparable, available) = match lines.next()? {
        "a newer release is available; run \"ccr update\" to install it" => (true, true),
        "already up to date" => (true, false),
        line if line.starts_with("current build is not a release version;") => (false, false),
        _ => return None,
    };
    Some(Candidate {
        current_version: current_version.into(),
        version: version.into(),
        comparable,
        available,
    })
}

async fn candidate(exe: &PathBuf, lang: &str) -> Result<Candidate, String> {
    let timeout = Duration::from_secs(45);
    let out = service::cc_remote(exe, &["update", "--check", "--json"], lang, timeout).await?;
    if out.code == Some(0) {
        return serde_json::from_str(&out.stdout).map_err(|error| error.to_string());
    }
    if !out
        .stderr
        .lines()
        .any(|line| line == "flag provided but not defined: -json")
    {
        return Err(out.message().into());
    }
    let out = service::cc_remote(exe, &["update", "--check"], "en", timeout).await?;
    if out.code != Some(0) {
        return Err(out.message().into());
    }
    legacy_candidate(&out.stdout).ok_or_else(|| {
        message(
            lang,
            "Could not read the Core update check result.",
            "无法读取 Core 更新检查结果。",
        )
        .into()
    })
}

/// A waiting restart can last as long as the session. Once it begins, bound
/// verification so a failed start is shown as an error.
async fn monitor(app: AppHandle, lang: String, target: String, previous: Local) {
    let mut restarting_since = None;
    loop {
        let local = match discover(&lang).await {
            Ok(Discovery::Managed(_, local)) => Some(local),
            _ => None,
        };
        let phase = local.as_ref().map_or(Phase::Restarting, |local| {
            local.activation(&target, &previous)
        });
        if phase == Phase::Waiting {
            restarting_since = None;
        }
        if phase == Phase::Restarting
            && restarting_since.get_or_insert_with(Instant::now).elapsed()
                >= Duration::from_secs(90)
        {
            core_error(&app, message(&lang,
                "Core did not come back on the installed version. Check the local service status and logs.",
                "Core 未能以已安装版本恢复运行。请检查本机服务状态和日志。").into(), local.as_ref());
            return;
        }
        change(&app, |view| {
            if let Some(local) = &local {
                local.record(view);
            }
            view.phase = phase;
            if phase == Phase::Updated {
                view.version = None;
            }
        });
        if phase == Phase::Updated {
            return;
        }
        tokio::time::sleep(Duration::from_secs(if phase == Phase::Waiting {
            8
        } else {
            3
        }))
        .await;
    }
}

fn track_activation(
    app: &AppHandle,
    lang: String,
    target: String,
    current: Option<&Local>,
    previous: Local,
) -> View {
    let phase = current.map_or(Phase::Restarting, |local| {
        local.activation(&target, &previous)
    });
    let status = change(app, |view| {
        if let Some(local) = current {
            local.record(view);
        }
        view.phase = phase;
        view.version = (phase != Phase::Updated).then(|| target.clone());
        view.error = None;
    });
    if phase.activating() {
        tauri::async_runtime::spawn(monitor(app.clone(), lang, target, previous));
    }
    status
}

#[tauri::command]
pub async fn core_update_status(app: AppHandle, lang: String) -> View {
    let state = app.state::<CoreUpdate>();
    let Ok(_guard) = state.operation.try_lock() else {
        return state.view();
    };
    if state.view().phase.activating() {
        return state.view();
    }
    match discover(&lang).await {
        Ok(Discovery::Managed(_, local)) => {
            if let Some(target) = local
                .installed_version
                .clone()
                .filter(|target| local.restart_pending == Some(true) && !local.active(target))
            {
                return track_activation(&app, lang, target, Some(&local), local.clone());
            }
            change(&app, |view| {
                if matches!(view.phase, Phase::Unmanaged | Phase::Unavailable)
                    || view.installed_version != local.installed_version
                {
                    view.phase = Phase::Idle;
                    view.version = None;
                    view.error = None;
                }
                local.record(view);
            })
        }
        Ok(Discovery::Unavailable(phase)) => unavailable(&app, phase),
        Err(error) => core_error(&app, error, None),
    }
}

#[tauri::command]
pub async fn core_update_check(app: AppHandle, lang: String) -> View {
    let state = app.state::<CoreUpdate>();
    let Ok(_guard) = state.operation.try_lock() else {
        return state.view();
    };
    if state.view().phase.activating() {
        return state.view();
    }
    change(&app, |view| {
        view.phase = Phase::Checking;
        view.version = None;
        view.error = None;
    });
    let (exe, local) = match discover(&lang).await {
        Ok(Discovery::Managed(exe, local)) => (exe, local),
        Ok(Discovery::Unavailable(phase)) => return unavailable(&app, phase),
        Err(error) => return core_error(&app, error, None),
    };
    match candidate(&exe, &lang).await {
        Ok(result) if result.comparable => change(&app, |view| {
            local.record(view);
            view.installed_version = Some(result.current_version);
            if result.available {
                view.phase = Phase::Available;
                view.version = Some(result.version);
            } else {
                view.phase = Phase::Updated;
            }
        }),
        Ok(_) => core_error(&app, message(&lang,
            "This Core build has no release version. Install a release build to enable updates.",
            "当前 Core 构建没有正式版本号。请先安装发布版以启用更新。").into(), Some(&local)),
        Err(error) => core_error(&app, error, Some(&local)),
    }
}

#[tauri::command]
pub async fn core_update_install(
    app: AppHandle,
    lang: String,
    version: String,
) -> Result<View, String> {
    let installation = app.state::<ComponentInstallLock>();
    let _install = installation
        .0
        .try_lock()
        .map_err(|_| "Another component is being installed")?;
    let state = app.state::<CoreUpdate>();
    let _guard = state
        .operation
        .try_lock()
        .map_err(|_| "A Core update operation is already running")?;
    let view = state.view();
    if !matches!(view.phase, Phase::Available | Phase::Error)
        || view.version.as_deref() != Some(&version)
    {
        return Err("Check for this Core version before installing it".into());
    }
    change(&app, |view| {
        view.phase = Phase::Installing;
        view.error = None;
    });
    let (exe, previous) = match discover(&lang).await {
        Ok(Discovery::Managed(exe, local)) => (exe, local),
        Ok(Discovery::Unavailable(phase)) => return Ok(unavailable(&app, phase)),
        Err(error) => return Ok(core_error(&app, error, None)),
    };
    let out = service::cc_remote(
        &exe,
        &["update", "--version", &version],
        &lang,
        Duration::from_secs(12 * 60),
    )
    .await;
    let current = match discover(&lang).await {
        Ok(Discovery::Managed(_, local)) => Some(local),
        _ => None,
    };
    match out {
        Ok(out) if out.code == Some(0) => {}
        Ok(out) => return Ok(core_error(&app, out.message().into(), current.as_ref())),
        Err(error) => return Ok(core_error(&app, error, current.as_ref())),
    }
    // The CLI can exit successfully without changing anything, e.g. when
    // another updater has already installed a newer version.
    if current
        .as_ref()
        .is_some_and(|local| !local.installed(&version))
    {
        return Ok(core_error(
            &app,
            message(
                &lang,
                "Could not confirm the selected version was installed. Check for updates again.",
                "无法确认目标版本已安装。请重新检查更新。",
            )
            .into(),
            current.as_ref(),
        ));
    }
    Ok(track_activation(
        &app,
        lang,
        version,
        current.as_ref(),
        previous,
    ))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn compatibility_check_reads_the_legacy_cli() {
        let result = legacy_candidate("current version: 0.42.0\r\nrelease version: v0.43.0 (via GitHub API (anonymous))\r\na newer release is available; run \"ccr update\" to install it\r\n").unwrap();
        assert_eq!(result.current_version, "0.42.0");
        assert_eq!(result.version, "v0.43.0");
        assert!(result.comparable && result.available);
        assert!(legacy_candidate("release lookup failed").is_none());
    }

    #[test]
    fn a_staged_version_is_only_active_after_the_new_service_answers() {
        let old = Local {
            running: true,
            running_version: Some("0.42.0".into()),
            pid: Some(5),
            ..Local::default()
        };
        let mut staged = Local {
            installed_version: Some("0.43.0".into()),
            running: true,
            running_version: old.running_version.clone(),
            pid: old.pid,
            ..Local::default()
        };
        let target = "v0.43.0";
        assert!(staged.installed(target));
        assert_eq!(staged.activation(target, &old), Phase::Waiting);
        staged.running = false;
        assert_eq!(staged.activation(target, &old), Phase::Restarting);
        staged.running = true;
        staged.running_version = Some("0.43.0".into());
        assert_eq!(staged.activation(target, &old), Phase::Updated);
    }
}
