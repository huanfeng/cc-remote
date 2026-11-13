# CC Remote Tray

独立的 wind-ui-rust 原生托盘。常驻与悬停不创建 WebView；点击托盘打开原生摘要窗口，点击「详细历史」才启动 Tauri 桌面客户端。采样、账号识别和 SQLite 历史均由 cc-remote 后台负责，退出托盘后后台仍继续采样。

## 一键安装与更新（Windows x64）

```powershell
irm https://github.com/huanfeng/cc-remote/releases/latest/download/install-desktop.ps1 | iex
```

一次安装后台服务、桌面客户端和托盘，创建开始菜单快捷方式并仅启动托盘。重复执行即可更新三者，保留配置、账号和历史；`cc-remote update` 仅更新后台。

需要登录 Windows 时自动启动托盘：

```powershell
& ([scriptblock]::Create((irm https://github.com/huanfeng/cc-remote/releases/latest/download/install-desktop.ps1))) -AutoStart
```

自启为可选项，后续更新保留已有自启设置；`-NoLaunch` 可跳过托盘启动或重启。其它参数见[发布与安装](../docs/release.md)。

## 构建与运行（Windows）

需要 Rust MSVC 工具链。依赖 windui 固定在 Cargo.toml 中的 Git revision。

```powershell
cargo test --locked --manifest-path tray/Cargo.toml
cargo build --release --locked --manifest-path tray/Cargo.toml
./tray/target/release/cc-remote-tray.exe
```

默认调用本机 `cc-remote service status --json` 发现后台，并从它的数据目录读取 `access-token`。托盘每 30 秒读取缓存，不因此增加官方额度接口请求。后台版本需包含 `/api/usage/monitor`。

开发时可以连接另一份本机后台，并指定新版桌面客户端：

```powershell
./tray/target/release/cc-remote-tray.exe --host http://127.0.0.1:8790 --data-dir D:/cc-remote-data --desktop D:/Develop/workspace/cc-remote/desktop/src-tauri/target/release/cc-remote-desktop.exe
```

`--host` 和 `--data-dir` 必须同时提供，数据目录必须为绝对路径；只接受 loopback HTTP(S) origin。访问令牌每次请求重新读取，令牌轮换后无需重启托盘。`--cc-remote` 可指定用于发现服务的后台可执行文件。历史窗口通过 `--usage-history --host … --data-dir … --account …` 唤起，命令行和链接不携带令牌。

可执行文件查找顺序：显式路径、托盘所在目录、开发仓库构建产物、常见安装目录、PATH。安装时建议将托盘、后台、桌面客户端放在同一目录，或指定显式路径。

在摘要里「关注」一个账号后，悬停显示该账号的主要额度；选择保存在用户配置目录 `cc-remote-tray/preferences.json`。后台离线时保留上次快照并标为过期。采样间隔可以设置为 10 或 30 分钟，历史保留 30 天。

`--check` 可读取缓存并输出不含凭证的 JSON；`--smoke-seconds N` 用于验证原生托盘启动后自动退出。Windows 是本次验证平台，其它系统尚未做运行验证。从源码构建或手动解压时需自行启动；一键安装入口会启动托盘，并可选创建登录自启项。

## CI 与发布

PR / push 到 `main` 会构建 Windows x64 托盘和 Tauri 客户端、运行 Rust 测试并打包。推送 `vX.Y.Z` 标签或手动运行 Release 后，同一个 GitHub Release 提供 `cc-remote-tray_<版本>_windows_amd64.zip`，以及包含后台、客户端、托盘的 `cc-remote-desktop_<版本>_windows_amd64.zip`。归档和桌面安装包纳入 `checksums.txt`。托盘 ZIP 适合已有后台和桌面客户端的用户；完整便携包解压后按 `START-HERE.txt` 安装后台，再启动托盘。

账号连续性及接口说明见 [订阅监控](../docs/subscription-monitor.md)。
