# First-use guide

[Back to README](../README.en.md) · [简体中文](getting-started.md)

Get one task working locally, then connect a phone or another computer. The **host** is the computer running your tasks: its project directories, CLI accounts and CC Remote service are the ones you use.

## 1. Prepare the host

Install [Claude Code](https://code.claude.com/docs) or [Codex CLI](https://developers.openai.com/codex/cli) and sign in under the system account that will run CC Remote. You only need the provider you intend to use.

```sh
claude auth login  # Claude Code
codex login        # Codex
```

Confirm that the chosen CLI works in a normal terminal first. CC Remote uses the host's CLI configuration, login and project files; it does not supply a model account.

## 2. Install and check the host service

### Windows x64, including the client and tray

In a normal PowerShell 5.1 / 7 terminal:

```powershell
irm https://github.com/huanfeng/cc-remote/releases/latest/download/install-desktop.ps1 | iex
```

The tray starts after installation: **hover to see usage, click to open the client, right-click for its menu**. To start it at Windows sign-in:

```powershell
& ([scriptblock]::Create((irm https://github.com/huanfeng/cc-remote/releases/latest/download/install-desktop.ps1))) -AutoStart
```

For Windows ARM64, or if you only want the backend:

```powershell
irm https://github.com/huanfeng/cc-remote/releases/latest/download/install.ps1 | iex
```

### Linux / macOS

```sh
curl -fsSL https://github.com/huanfeng/cc-remote/releases/latest/download/install.sh | sh
```

### Verify installation

Open a new terminal and run:

```sh
ccr --version
ccr status
```

Check that the service is listening at `http://127.0.0.1:8787`. If you configured another port, use the address shown by status. If the command is missing or the service is stopped, see [troubleshooting](#if-something-goes-wrong).

The installer downloads the latest stable release, checks SHA256 and registers a service for your current user. Manual downloads, custom paths and installation without a service are covered in [releases and installation](release.md) (Chinese).

## 3. Connect and start a task

### Windows client

Click the tray icon or launch **CC Remote** from the Start menu. Connect through **Local service**; the client reads the token from the service's data directory. For another computer's host, enter that host's address and token.

### Browser connection and access token

On the **host computer**, open <http://127.0.0.1:8787>, read the security notice and enter the access token. Read it from the default data directory:

Windows (PowerShell):

```powershell
Get-Content "$env:APPDATA\cc-remote\access-token"
```

Linux:

```sh
cat "${XDG_CONFIG_HOME:-$HOME/.config}/cc-remote/access-token"
```

macOS:

```sh
cat "$HOME/Library/Application Support/cc-remote/access-token"
```

If you changed the data directory, use the path reported by `ccr status`. If you configured the token through an environment variable, use that value. Never include your token in issues, screenshots or shared links.

### Complete your first task

1. Click **New** and choose **Claude Code** or **Codex**.
2. Enter an **existing project directory on the host**. A directory on your phone or another computer is not the host's project path.
3. Choose the model and permissions; defaults are fine for a first task. Try: `Explore this project read-only. Explain its main directories, technology stack and entry points. Do not modify files yet.`
4. Follow the streamed reply and tool activity. Answer approvals or questions when they appear.

Once you see a reply, the local setup works. The session runs on the host, so you can close the browser and come back later.

### Claude Code terminal handoff

Run `ccr` in a project to select a terminal session or create one. Press **Ctrl+]** to detach without stopping the process. Open that session in the web UI to continue, then run `ccr` to reconnect.

```sh
cd your-project
ccr
ccr ls                # browse recent terminal sessions
ccr claude resume <id>     # resume a Claude Code session
```

`ccr` and `cc-remote` are interchangeable. **Terminal handoff is for Claude Code**. For Codex, create a managed session in the web / desktop client, or import a session from **Terminal history**. Before importing native history, stop the original session in its terminal to avoid running the same session twice.

## Connect a phone or another computer

### On the same LAN

The default listener accepts local connections only. Add LAN access on the host:

```sh
ccr config --listen 127.0.0.1:8787,lan:8787
```

Join the same trusted LAN, then open `http://<host-LAN-IP>:8787` on the phone and use the same access token. **Do not enter `127.0.0.1` on your phone**: it points to the phone itself. The host firewall must allow the port; guest Wi-Fi or AP isolation can prevent devices from connecting.

LAN HTTP transmits the token and conversation unencrypted. The token grants code execution on the host; only use this on a trusted network. See [configuration](configuration.md) and [security](security.md) (Chinese).

### Away from the LAN

Use your own VPN, HTTPS tunnel or reverse proxy to make the host reachable. CC Remote does not provide NAT traversal or TLS certificates. A reverse proxy must also forward WebSocket connections for the terminal. See [reverse-proxy configuration](security.md#反向代理) (Chinese).

### Public web client

The [public web client](https://huanfeng.github.io/cc-remote/) lets you add several hosts in one interface. Your hosts must still be running and reachable. When this HTTPS page connects to a remote host, the host also needs HTTPS and must allow the origin `https://huanfeng.github.io`. Local loopback addresses are an exception. See [standalone frontend requirements](standalone.md) (Chinese).

For one host, you can simply use its built-in web UI without deploying a separate frontend. The Windows client can also save several hosts; see the [desktop guide](desktop.md) (Chinese).

## Updates and everyday management

```sh
ccr status       # inspect status
ccr logs -f      # follow logs
ccr restart      # restart the backend
ccr update       # CLI/backend only; waits for idle sessions before restarting
```

To update the Windows client and tray, repeat the full installation command. Accounts, history and configuration are kept. Exit the running client from its tray menu and reopen it for the new version.

The backend and desktop client run separately. Quota sampling continues after you quit the tray; stopping the backend affects sessions it manages. See [subscription monitoring](subscription-monitor.md) (Chinese).

## If something goes wrong

| Symptom | Check first |
| --- | --- |
| `ccr` is not found | Open a new normal terminal; check that the installation directory is in your user PATH |
| Local page will not open | Run `ccr status`; if stopped, run `ccr start`, then inspect `ccr logs -f` |
| Token is invalid or missing | Make sure the address and data directory belong to the same host; use the status output for custom directories |
| Phone cannot connect | Test locally first, then check `lan:8787`, the host LAN IP, firewall and Wi-Fi isolation |
| Public page loads but host connection fails | Check host HTTPS, network access and allowed origins; see [standalone frontend](standalone.md) |
| CLI works in the terminal but fails in the service or returns 403 | Check the system account, CLI path and proxy; services do not load your shell profile |
| Windows service appears to use a different data directory | Use normal PowerShell / cmd instead of an MSIX app's built-in terminal, where APPDATA may be redirected |
| Terminal session has no web question / plan cards | Check whether Claude Code hooks are disabled or restricted by organization policy; see [usage](usage.md) |

If your proxy only exists in your shell profile, run `ccr config --capture-env` from a normal terminal with that proxy set, or configure the service environment explicitly as described in [configuration](configuration.md).

Next steps (Chinese): [Usage](usage.md) · [Codex](codex.md) · [Supplier profiles](supplier-profiles.md) · [Subscription monitoring](subscription-monitor.md) · [Back to README](../README.en.md)
