[简体中文](README.md) | English

# CC Remote

Use the Claude Code on your own machine from your phone or any browser — the terminal and the web UI share the same session, so you can pick up right where you left off.

[![CI](https://github.com/huanfeng/cc-remote/actions/workflows/ci.yml/badge.svg)](https://github.com/huanfeng/cc-remote/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/huanfeng/cc-remote?include_prereleases)](https://github.com/huanfeng/cc-remote/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

<p align="center">
  <img src="docs/images/desktop.png" alt="CC Remote on desktop" width="74%">
  &nbsp;
  <img src="docs/images/mobile.png" alt="CC Remote on a phone" width="22%">
</p>

CC Remote is a small host program (a single executable) that runs on your computer, drives the Claude Code CLI you are already logged in to, and serves a mobile-friendly web UI (PWA). No terminal emulator, and your code never leaves your machine.

> The web UI is currently in Chinese.

## Features

- **Share a terminal session with the web**: run `ccr` in a project to start native Claude Code; the web UI talks to that very process, so you can keep going away from your desk.
- **Managed sessions**: start sessions from the browser in any project directory, run several in parallel, and close the tab without stopping them.
- **Answer approvals, questions and plans from the web**: tool permissions, AskUserQuestion choices and plan approvals show up as cards; in shared sessions, whichever side answers first wins.
- **Per-session model and permission mode**: every permission mode (including `bypassPermissions`, behind a confirmation) and reasoning effort, without touching your `~/.claude/settings.json`.
- **Attachments**: paste, drop or pick files to send to Claude, with thumbnails for images.
- **Background service with auto start**: one command registers a per-user service (Windows Task Scheduler / systemd / launchd) and adds the `ccr` shortcut.
- **Status and usage**: a status panel with uptime, cost, tokens, a 14-day chart and the log tail.
- **Built for phones**: responsive layout, drawer session list, installable as a PWA.
- **Survives a refresh**: the open session, unsent drafts and attachments, and half-filled answers come back after reloading.

## Quick start

**Requirements**: [Claude Code](https://code.claude.com/docs) installed and logged in (`claude auth login`), on Windows, Linux or macOS.

### 1. Install

Windows (PowerShell):

```powershell
irm https://github.com/huanfeng/cc-remote/releases/latest/download/install.ps1 | iex
```

Linux / macOS:

```sh
curl -fsSL https://github.com/huanfeng/cc-remote/releases/latest/download/install.sh | sh
```

The script downloads the latest stable release, verifies its sha256, installs and starts the background service, and creates the `ccr` command. Run it again to update; your settings are kept.

To pass options (for example, to allow LAN access):

```powershell
& ([scriptblock]::Create((irm https://github.com/huanfeng/cc-remote/releases/latest/download/install.ps1))) -Listen lan:8787
```

```sh
curl -fsSL https://github.com/huanfeng/cc-remote/releases/latest/download/install.sh | sh -s -- --listen lan:8787
```

Other options: `-Version` / `--version`, `-NoService` / `--no-service`, `-NoAlias` / `--no-alias`, `-DryRun` / `--dry-run`. See [docs/release.md](docs/release.md) (Chinese).

### 2. Open the web UI and log in

Open <http://127.0.0.1:8787> and enter the access token. It is generated on first start and stored in the `access-token` file in the data directory:

```powershell
# Windows
Get-Content "$env:APPDATA\cc-remote\access-token"
```

```sh
# Linux
cat "${XDG_CONFIG_HOME:-$HOME/.config}/cc-remote/access-token"
# macOS
cat "$HOME/Library/Application Support/cc-remote/access-token"
```

### 3. Run `ccr` in a project

```sh
cd your-project
ccr
```

Claude Code opens in the terminal as usual, and the session appears in the web UI. The first run asks you to confirm loading the development channel in the terminal. (If `ccr` is not found right after installing, open a new terminal.)

## Usage

### Shared terminal (ccr)

`ccr` is short for `cc-remote -- claude`; arguments go straight to Claude Code, e.g. `ccr --model sonnet` or `ccr --resume <session-uuid>`. Terminal and web use the same process and context, and leaving the web UI does not affect the terminal. Stopping generation and native commands such as `/compact` stay in the terminal.

### Managed sessions

Click "新建" (New) in the web UI and enter a name and an existing project directory on the host. Each turn runs Claude Code on the host; you can stop it at any time and the next turn resumes the context. "终端历史" (Terminal history) lists existing Claude Code sessions on the machine and can import them as managed sessions.

More — web commands (`/model`, `/effort`, `/status` …), attachments, settings dialogs, refresh recovery — in [docs/usage.md](docs/usage.md) (Chinese).

### Phone / LAN access

The host listens on `127.0.0.1` only by default. To reach it from your phone over the LAN:

```sh
cc-remote service config --listen 127.0.0.1:8787,lan:8787
```

Then open `http://<your computer's LAN IP>:8787` on the phone.

> The access token is effectively the right to run code on your computer, and LAN access is plain HTTP. Use it only on networks you trust, or put it behind an HTTPS reverse proxy or tunnel. See [docs/security.md](docs/security.md) (Chinese).

### Managing the service

| Command | What it does |
| --- | --- |
| `cc-remote service install` | Install and start the service (re-run to reinstall) |
| `cc-remote service status` | Show whether it is running |
| `cc-remote service logs -f` | Print / follow the log |
| `cc-remote service restart` | Restart (also `start` / `stop`) |
| `cc-remote service config` | Show or change listen addresses, environment, … |
| `cc-remote service uninstall` | Remove the service (data is kept) |
| `cc-remote update` | Update to the latest release (`--check` to only check) |

## Configuration and docs

The detailed docs are in Chinese.

| Document | Covers |
| --- | --- |
| [docs/usage.md](docs/usage.md) | Detailed usage |
| [docs/configuration.md](docs/configuration.md) | CLI flags, data directory, environment variables, listen addresses, service environment, permission and model preferences |
| [docs/security.md](docs/security.md) | Security model and recommendations |
| [docs/architecture.md](docs/architecture.md) | How it works, known limitations |
| [docs/release.md](docs/release.md) | Installer options, updating, uninstalling, releasing, troubleshooting |
| [docs/development.md](docs/development.md) | Building from source, tests, CI |

## FAQ

**Sessions in the service fail with `API Error: 403 Request not allowed`, but `claude` works in my terminal.**
The background service does not load your shell profile, so a proxy set in `$PROFILE` / `~/.bashrc` is missing. Run `cc-remote service config --capture-env` from a terminal where the proxy is set, or set it explicitly with `--env HTTPS_PROXY=...`.

**Where is the token? I lost it.**
In the `access-token` file in the data directory (paths under Quick start). Delete the file and restart the service to generate a new one.

**On Windows the service doesn't see my data / `ccr` can't connect.**
If you ran cc-remote from the built-in terminal of an MSIX-packaged app (some desktop AI clients), `%APPDATA%` was redirected into that app's private folder. Run `ccr` and `cc-remote` from a regular PowerShell or cmd window.

**No question / plan cards in the web UI for a shared session?**
That relies on a Claude Code hook. With `disableAllHooks` or an organization policy that only allows managed hooks, questions and plan approvals can only be answered in the terminal.

## Building from source

Requires Go 1.26+, Node.js 22 and pnpm (`corepack enable`):

```sh
pnpm --dir web install --frozen-lockfile
pnpm --dir web run build
go build -o cc-remote ./cmd/cc-remote
```

Tests, the dev server and CI are covered in [docs/development.md](docs/development.md) (Chinese).

## License

[MIT](LICENSE)
