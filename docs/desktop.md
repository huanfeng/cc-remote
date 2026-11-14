# 桌面应用（Tauri 2）

`desktop/` 是 CC Remote 的桌面应用：把 `web/` 的前端装进原生窗口，可以保存多台宿主（和[独立网页前端](standalone.md)一样），并管理本机安装的 cc-remote 服务。Windows x64 推荐用完整安装入口一次安装后台和内置托盘的客户端：

```powershell
irm https://github.com/huanfeng/cc-remote/releases/latest/download/install-desktop.ps1 | iex
```

重复执行即可一起更新，配置、账号和历史保留；已打开的客户端需从托盘菜单退出后重新打开以使用新版。安装后默认仅启动不加载 WebView 的托盘，从开始菜单或托盘「详细历史」打开客户端。需要托盘登录自启时，以 scriptblock 方式传入 `-AutoStart`；完整参数见[发布与安装](release.md#一键安装--更新)。

Release 工作流发布 Windows x64 的 NSIS 安装包和便携包，版本号与后台一致。安装包只安装桌面客户端；便携包同时包含后台和内置托盘的客户端。PR / push 到 `main` 会运行原生测试并生成同样的 CI 构建附件。文件名和发布命令见[发布说明](release.md#发布附件)。

常驻托盘已集成到桌面客户端，由 Tauri 管理。单击托盘打开或恢复 CC Remote 主窗口；右键可刷新用量、查看全部历史或退出。Windows 悬停约 400 ms 后显示 wind-ui-rust 原生紧凑用量面板，收藏账号优先，支持多选收藏、额度分色、刷新按钮加载状态和 toast，不加载 WebView。可移入面板操作，移出图标和面板后自动收起；打开时不抢键盘焦点，且不出现在任务栏。关闭按钮或 Escape 收起后，移出再悬停才重新显示。详细历史仍在主窗口查看。`cc-remote-desktop --tray` 只启动托盘与后台缓存读取，打开主窗口时才创建 WebView。关闭主窗口后隐藏到托盘，退出请使用托盘菜单，后台服务继续记录。

托盘每 30 秒读取本机后台的用量缓存，只有手动刷新才调用刷新接口；OAuth、采样和 SQLite 历史继续由后台负责。收藏保存在桌面应用配置目录的 `tray-preferences.json`，首次使用兼容旧工具的多选或单账号收藏。旧 wind-ui-rust 工具源码已移出本仓库，单独存放于同级的 `cc-tray/` 目录，不参与 CC Remote 构建和发布；独立仓库和直接读取提供方用量的功能尚未实现。

桌面客户端仍接收 `--usage-history --host <本机 origin> --data-dir <绝对路径> --account <可选账号 ID>`，已运行时聚焦原窗口并定位历史页。令牌通过本机数据目录读取，不进入启动参数。采样和账号连续性见[订阅监控](subscription-monitor.md)。

## 与独立网页的区别

| | 独立网页 | 桌面应用 |
| --- | --- | --- |
| 访问宿主 | 浏览器直连，宿主要允许页面来源（`--allow-origin`），且须 HTTPS | 由 Rust 发出请求（不带 Origin，没有 CORS 限制），宿主**无需任何配置**，局域网 `http://` 宿主也能连 |
| 终端 | 浏览器 WebSocket | Rust 侧 WebSocket（`ws:`/`wss:` 都可） |
| 本机服务 | — | 「本机服务」：查看状态、启动 / 停止 / 重启、一键连接（令牌从服务的数据目录读取） |
| 窗口 | 浏览器标签页 | Windows / Linux 使用系统标题栏，首次启动也能拖动、最小化和最大化；macOS 保留原生红绿灯；在宿主列表里 Ctrl+点击在新窗口打开 |
| 链接 | 新标签页 | 在系统浏览器中打开 |
| 路径菜单 | 复制完整路径、名称或上级目录 | 连接本机回环地址的宿主时，还可打开目录、在文件管理器中定位、在该目录打开终端 |

令牌和草稿保存在应用的 WebView 存储里（Windows：`%LOCALAPPDATA%\io.github.huanfeng.ccremote`）。通过明文 `http://` 连接其它电脑上的宿主时，令牌会在网络中明文传输，只应在可信网络中这样使用。

会话顶栏的供应商可直接点击切换，操作方式与模型选择一致；如果会话仍在执行，会先确认中断。目录标签和文件预览里的路径按钮打开操作菜单。系统路径操作仅对 `localhost`、`127.x.x.x` 或 `::1` 的本机宿主显示，远程宿主提供复制操作；目录必须在客户端电脑上实际存在。

左侧项目名称使用更大的加粗文字，列表顶部的「进行中」集中显示运行中、等待输入和仍有后台任务的会话。这些会话同时保留在原项目下，可以按标题或路径筛选。

## 结构

- `web/src/platform/desktop.ts`：前端里的桌面桥接。`main.tsx` 最先导入它（`lib/hosts.ts` 加载时就要知道是不是桌面应用）。它把跨源的 `fetch` 转给 Rust，提供 `DesktopSocket`（终端），接管外部链接和 `window.open`，并在 `<html>` 上加 `desktop-shell` 和 `platform-<系统>-desktop` 类。
- `web/src/platform/localService.ts`、`web/src/features/desktop/LocalService.tsx`：本机服务（首页卡片和设置里的「本机服务」分区）。
- `desktop/src-tauri/src/net.rs`：HTTP 转发。请求体就是 invoke 的原始字节，方法、地址和请求头放在 `x-cc-meta` 请求头里（百分号编码的 JSON）；回复是 `[元数据长度 u32 大端][元数据 JSON][响应体]`。页面不能设置 `Origin`、`Host`、`Cookie` 等请求头。
- `desktop/src-tauri/src/ws.rs`：WebSocket 转发，事件经 Channel 回到页面（二进制帧原样，其余为 JSON）。
- `desktop/src-tauri/src/service.rs`：调用本机安装的 `cc-remote`（先找服务安装目录里的副本，再找 PATH）：`service status --json`、`service start|stop|restart`，以及读取数据目录里的 `access-token`。
- `desktop/src-tauri/src/paths.rs`、`web/src/platform/pathActions.ts`：本机路径菜单，校验回环宿主、已有绝对路径及本机驱动器，使用独立参数和工作目录调用系统程序。
- `desktop/src-tauri/src/usage_tray/`：Tauri 托盘、缓存读取、收藏持久化、历史导航；`panel.rs` 使用 wind-ui-rust 渲染原生用量窗口，`hover.rs` 管理独立 UI 线程和悬停生命周期。

## 开发

需要 Rust（MSVC 工具链）、Node 22 和 pnpm；Windows 需要 WebView2 运行时（Windows 11 自带）。

```sh
cd web
corepack enable
pnpm install --frozen-lockfile
cd ../desktop
pnpm install --frozen-lockfile
pnpm dev      # 启动 vite（5175 端口，--mode desktop）并运行应用
# 或 pnpm build，构建安装包（Windows：NSIS 与 MSI）
```

`web` 里的 `pnpm run build:desktop` 单独构建前端到 `web/dist-desktop/`，内容安全策略由 `desktop/src-tauri/tauri.conf.json` 提供（所有网络请求都走 IPC，`connect-src` 只放行 `ipc:`）。

测试：

```sh
cd desktop/src-tauri
cargo test                                                    # 转发命令的离线检查
CC_TEST_HOST=http://127.0.0.1:8790 CC_TEST_TOKEN=<令牌> cargo test   # 再连一台开发宿主：不带 Origin、ETag 304
CC_TEST_LOCAL_SERVICE=1 cargo test local_status               # 读取本机服务状态（只读）
```

Windows 正式版启动回归检查（先退出其它 desktop 实例，在仓库根目录运行）：

```powershell
./scripts/test-desktop-startup.ps1
```

该检查验证 `--tray` 常驻时不创建 WebView，再通过第二次启动打开并反复唤起主窗口，检查窗口响应和 WebView 子进程；结束时只停止检查启动的实例。窗口创建与激活由专用线程串行处理，避免 Windows 同步托盘／单实例回调中的 WebView2 死锁。

调试窗口里的页面：设置 `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=9222` 后运行，用 Edge 打开 `edge://inspect`。远程桌面断开（`quser` 显示 `Disc`）时，窗口里的页面不会渲染，也不响应调试协议。

Windows 原生面板的专项检查（在交互桌面上单独运行）：

```powershell
cargo test --locked --manifest-path desktop/src-tauri/Cargo.toml native_hover_runtime -- --ignored --test-threads=1
cargo test --locked --manifest-path desktop/src-tauri/Cargo.toml render_native_panel_fixtures -- --ignored --test-threads=1
```

第一项通过应用通道连续打开、关闭原生窗口，验证工具窗口样式与前台焦点保持；不依赖定时器掩盖关闭消息。第二项生成正常和离线场景截图到 `desktop/src-tauri/target/native-panel-fixtures/`。
