# 桌面应用（Tauri 2）

`desktop/` 是 CC Remote 的桌面应用：把 `web/` 的前端装进原生窗口，可以保存多台宿主（和[独立网页前端](standalone.md)一样），并管理本机安装的 cc-remote 服务。Windows x64 推荐用完整安装入口一次安装后台、客户端与托盘：

```powershell
irm https://github.com/huanfeng/cc-remote/releases/latest/download/install-desktop.ps1 | iex
```

重复执行即可一起更新，配置、账号和历史保留；已打开的客户端需关闭后重新打开以使用新版。安装后默认仅启动不加载 WebView 的托盘，从开始菜单或托盘「详细历史」打开客户端。需要托盘登录自启时，以 scriptblock 方式传入 `-AutoStart`；完整参数见[发布与安装](release.md#一键安装--更新)。

Release 工作流发布 Windows x64 的 NSIS 安装包和便携包，版本号与后台、托盘一致。安装包只安装桌面客户端；便携包同时包含后台、客户端和托盘。PR / push 到 `main` 会运行原生测试并生成同样的 CI 构建附件。文件名和发布命令见[发布说明](release.md#发布附件)。

轻量常驻功能位于独立的 [wind-ui-rust 托盘](../tray/README.md)。桌面客户端按需展示完整历史，接收 `--usage-history --host <本机 origin> --data-dir <绝对路径> --account <可选账号 ID>`，已运行时聚焦原窗口并定位历史页。令牌通过本机数据目录读取，不进入启动参数。托盘自身不加载 WebView。采样和账号连续性见[订阅监控](subscription-monitor.md)。

## 与独立网页的区别

| | 独立网页 | 桌面应用 |
| --- | --- | --- |
| 访问宿主 | 浏览器直连，宿主要允许页面来源（`--allow-origin`），且须 HTTPS | 由 Rust 发出请求（不带 Origin，没有 CORS 限制），宿主**无需任何配置**，局域网 `http://` 宿主也能连 |
| 终端 | 浏览器 WebSocket | Rust 侧 WebSocket（`ws:`/`wss:` 都可） |
| 本机服务 | — | 「本机服务」：查看状态、启动 / 停止 / 重启、一键连接（令牌从服务的数据目录读取） |
| 窗口 | 浏览器标签页 | 自绘标题栏（Windows / Linux 右上角为窗口按钮，macOS 保留红绿灯）；在宿主列表里 Ctrl+点击在新窗口打开 |
| 链接 | 新标签页 | 在系统浏览器中打开 |

令牌和草稿保存在应用的 WebView 存储里（Windows：`%LOCALAPPDATA%\io.github.huanfeng.ccremote`）。通过明文 `http://` 连接其它电脑上的宿主时，令牌会在网络中明文传输，只应在可信网络中这样使用。

## 结构

- `web/src/platform/desktop.ts`：前端里的桌面桥接。`main.tsx` 最先导入它（`lib/hosts.ts` 加载时就要知道是不是桌面应用）。它把跨源的 `fetch` 转给 Rust，提供 `DesktopSocket`（终端），接管外部链接和 `window.open`，并在 `<html>` 上加 `desktop-shell` 和 `platform-<系统>-desktop` 类。
- `web/src/platform/localService.ts`、`web/src/features/desktop/LocalService.tsx`：本机服务（首页卡片和设置里的「本机服务」分区）。
- `desktop/src-tauri/src/net.rs`：HTTP 转发。请求体就是 invoke 的原始字节，方法、地址和请求头放在 `x-cc-meta` 请求头里（百分号编码的 JSON）；回复是 `[元数据长度 u32 大端][元数据 JSON][响应体]`。页面不能设置 `Origin`、`Host`、`Cookie` 等请求头。
- `desktop/src-tauri/src/ws.rs`：WebSocket 转发，事件经 Channel 回到页面（二进制帧原样，其余为 JSON）。
- `desktop/src-tauri/src/service.rs`：调用本机安装的 `cc-remote`（先找服务安装目录里的副本，再找 PATH）：`service status --json`、`service start|stop|restart`，以及读取数据目录里的 `access-token`。

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

调试窗口里的页面：设置 `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=9222` 后运行，用 Edge 打开 `edge://inspect`。远程桌面断开（`quser` 显示 `Disc`）时，窗口里的页面不会渲染，也不响应调试协议。
