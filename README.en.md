<p align="center"><img src="web/public/icon.svg" alt="CC Remote" width="72" height="72"></p>

<h1 align="center">CC Remote</h1>

<p align="center"><strong>Manage Claude Code sessions across multiple servers from one web page.</strong></p>

<p align="center">Work remotely, switch providers while keeping your conversation, and manage official OAuth tokens and subscription usage.</p>

<p align="center">
  <a href="https://github.com/huanfeng/cc-remote/actions/workflows/ci.yml"><img src="https://github.com/huanfeng/cc-remote/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/huanfeng/cc-remote/releases"><img src="https://img.shields.io/github/v/release/huanfeng/cc-remote" alt="Latest stable release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT License"></a>
</p>

<p align="center">
  <a href="README.md">简体中文</a> · English<br>
  <a href="#quick-start">Quick start</a> · <a href="docs/getting-started.en.md">First-use guide</a> · <a href="https://huanfeng.github.io/cc-remote/about/en/">Project home</a> · <a href="https://huanfeng.github.io/cc-remote/">Open web client</a> · <a href="https://github.com/huanfeng/cc-remote/releases/latest">Downloads</a>
</p>

<p align="center">
  <img src="docs/images/desktop.png" alt="Desktop interface example: sessions, tool results and questions awaiting an answer" width="74%">
  &nbsp;
  <img src="docs/images/mobile.png" alt="Phone interface example: follow a task and continue the conversation" width="22%">
</p>
<p align="center"><sub>Interface examples; appearance may differ in the current release.</sub></p>

## Main features

### 1. Manage sessions across servers from the web

Add your home computer, workstation and remote servers to one web client. Switch between servers and browse sessions by project. Start Claude Code sessions, continue conversations, read streaming replies and tool activity, and respond to approvals and questions from a desktop or phone browser.

Each server runs its own CC Remote backend and session processes. Closing the web page leaves tasks running on the server. Remote access requires a **reachable network address, HTTPS and an access token**. A standalone web client also needs its origin allowed by each server.

See the [connection guide](docs/getting-started.en.md#connect-a-phone-or-another-computer) and [standalone web client and HTTPS setup](docs/standalone.md) (Chinese).

### 2. Switch providers while keeping your session

Read **CC-Switch** profiles directly from the server. Choose a provider when creating a session, or change the provider for an existing Claude Code session. CC Remote keeps the session ID and conversation history, stops the old process, and automatically resumes with the new provider when you send the next message.

Each session can use its own provider, and you can set a default for new sessions. Switching during an active task asks you to confirm the interruption. The previous request is not automatically sent again.

See [provider selection and switching](docs/supplier-profiles.md) (Chinese).

### 3. Manage official Claude Code OAuth tokens and usage

Add, rename, replace or remove **OAuth tokens** for multiple official accounts, then choose an account for each session through the provider selector. Generate tokens with `claude setup-token` and store them on the corresponding server. Sessions retain the server's native model, plugin, hook, MCP and permission settings.

Compare accounts' **5-hour and weekly subscription quotas, reset times and usage history**. The backend collects readings even when the web page or desktop client is closed. The Windows tray also provides a quick view of account quotas.

See [OAuth token setup](docs/supplier-profiles.md#oauth-令牌配置) and [subscription usage monitoring](docs/subscription-monitor.md) (Chinese).

## More ways to work

- **Claude Code status line**: one-click setup under Settings → Claude Code, with compact, color and multiline styles. Shows available provider, model, effort, context usage, session cost and 5-hour / 7-day limits. Missing fields are hidden; uninstall restores the previous configuration. See the [official schema](https://code.claude.com/docs/en/statusline).
- **Terminal/web handoff**: run `ccr` in a project to start a Claude Code session. Press `Ctrl+]` to detach, continue from the web, and reconnect with `ccr` later.
- **Windows desktop client**: keep several servers open in independent tabs, with back, forward, refresh and in-app updates. Hover over the built-in tray to check usage.
- **Mobile and PWA**: follow tasks, send messages and answer approvals from your phone, or install the web UI as a PWA.
- **Codex sessions**: start managed sessions and import local history to continue it. See the support table below for details.

CC Remote is self-hosted. Sessions use the CLI and configuration on your server; model requests go to your chosen provider.

## Choose your starting point

| What you want | Where to start |
| --- | --- |
| A Windows client with a usage tray | Use the **Windows full installer** below; x64 only |
| A host on Linux, macOS or Windows ARM64 | Install the backend and use its built-in web UI |
| Continue tasks from your phone | Install a host first, then follow the [phone connection guide](docs/getting-started.en.md#connect-a-phone-or-another-computer) |
| One web client for several hosts | [Open the public web client](https://huanfeng.github.io/cc-remote/) after checking the [connection requirements](docs/standalone.md) (Chinese) |

> The web client needs your own running host. The public page provides neither cloud execution nor a preconfigured account. The default host address accepts local connections only.

## Quick start

### 1. Prepare your local CLI

Install and sign in to [Claude Code](https://code.claude.com/docs) or [Codex CLI](https://developers.openai.com/codex/cli) under **the same system account that runs the host**. You only need the CLI you intend to use.

```sh
claude auth login  # for Claude Code
codex login        # for Codex
```

### 2. Install CC Remote

**Windows x64: backend + desktop client + built-in tray** (PowerShell 5.1 / 7):

```powershell
irm https://github.com/huanfeng/cc-remote/releases/latest/download/install-desktop.ps1 | iex
```

**Linux / macOS: backend + web UI**:

```sh
curl -fsSL https://github.com/huanfeng/cc-remote/releases/latest/download/install.sh | sh
```

<details>
<summary>Windows backend only (x64 / ARM64)</summary>

```powershell
irm https://github.com/huanfeng/cc-remote/releases/latest/download/install.ps1 | iex
```

</details>

The installer downloads the latest stable release, verifies SHA256, registers and starts a service for your current user, and creates the `ccr` command. Open a new terminal if `ccr` is not found after installation.

### 3. Start your first task

**Windows client**: click the tray icon or launch **CC Remote** from the Start menu, then connect through **Local service**.

**Browser**: open <http://127.0.0.1:8787> and follow the [token instructions](docs/getting-started.en.md#browser-connection-and-access-token) to connect. Click **New**, choose Claude Code or Codex, and select **an existing project directory on the host**.

**Claude Code terminal handoff**: run this in your project:

```sh
cd your-project
ccr
# Ctrl+] detaches while the task continues. Run ccr again to reconnect.
```

Once it works locally, [connect your phone or another computer](docs/getting-started.en.md#connect-a-phone-or-another-computer).

## Supported workflows

| Capability | Claude Code | Codex |
| --- | --- | --- |
| Start managed sessions in the web / desktop client | Yes | Yes |
| Streaming replies, tool activity, approvals and questions | Yes | Yes |
| Import local history and continue it | Yes | Yes |
| `ccr` terminal/web handoff and web terminal view | Yes | Not yet |
| Official subscription readings | Supported official logins / OAuth accounts | Local official login |

The backend supports **Windows, Linux and macOS**. Native client downloads and the graphical hover tray currently target **Windows x64**. Quotas depend on what the provider reports; API-key and third-party profiles may have no subscription readings. See [Codex](docs/codex.md), [subscription monitoring](docs/subscription-monitor.md) and [known limitations](docs/architecture.md#已知限制) (Chinese).

## A few commands for everyday use

`ccr` and `cc-remote` are interchangeable. Examples use the shorter name:

```sh
ccr                    # pick a session in this project, or create one
ccr ls                 # browse and attach to recent terminal sessions
ccr --resume <id>      # resume a Claude Code session
ccr -c                 # continue this directory's latest Claude Code session
ccr status             # inspect the host service
ccr logs -f            # follow host logs
ccr update             # update the CLI and backend only
```

Lists support arrow keys, numbers plus Enter, `/` to filter, `r` to refresh and `q` / Esc to quit. See [usage](docs/usage.md) (Chinese).

**To update the client and tray**, signed stable builds check automatically. Download and install updates from **… → About & updates**, Settings or the tray menu. You can also repeat the Windows full installation command; configuration, accounts and history are kept. `ccr update` updates the CLI and backend separately. See the [desktop guide](docs/desktop.md) (Chinese).

## Find your next step

The first-use guide is available in English; detailed reference docs are currently in Chinese.

| I want to… | Read |
| --- | --- |
| Go from installation to my first task | [First-use guide](docs/getting-started.en.md) |
| Find my token, connect a phone or troubleshoot | [Connection guide](docs/getting-started.en.md#connect-a-phone-or-another-computer) |
| Use sessions, approvals, attachments and file previews | [Usage](docs/usage.md) |
| Use Codex or continue local history | [Codex guide](docs/codex.md) |
| Switch providers while keeping a session, or configure official OAuth tokens | [Supplier profiles](docs/supplier-profiles.md) |
| Compare account quotas and history | [Subscription monitoring](docs/subscription-monitor.md) |
| Use the desktop app, tray and local path menu | [Desktop app](docs/desktop.md) |
| Configure proxies, models, services or listen addresses | [Configuration](docs/configuration.md) |
| Deploy a web client, HTTPS or a reverse proxy | [Standalone frontend](docs/standalone.md) · [Security](docs/security.md) |
| Download manually, update or uninstall | [Releases and installation](docs/release.md) |
| Build, develop or contribute | [Development](docs/development.md) · [Architecture](docs/architecture.md) |

## FAQ

**Can my phone open `127.0.0.1:8787`?** That address points to the phone itself. Use your host computer's LAN address or a configured remote address; see the [connection guide](docs/getting-started.en.md#connect-a-phone-or-another-computer).

**Will a task continue if I close the browser or detach?** Yes, while the host and its session process keep running. `Ctrl+]` detaches; use the session's stop action to interrupt work.

**The CLI works in my terminal but fails in the service.** Services do not load your shell profile, so their CLI paths, accounts or proxy environment may differ. Follow the [troubleshooting steps](docs/getting-started.en.md#if-something-goes-wrong).

> An access token grants code execution on the host. The default listener is local only; LAN HTTP is unencrypted. Use a trusted network or HTTPS for remote access and protect your token. See [security](docs/security.md).

## Contribute

Report problems in [Issues](https://github.com/huanfeng/cc-remote/issues) with the version, operating system, reproduction steps and logs with secrets removed. Suggestions and improvements are welcome; start with the [development guide](docs/development.md).

If CC Remote helps you take work from your desk to your phone, share the project or leave a Star so other Claude Code and Codex users can find it.

[MIT License](LICENSE)
