# 桌面应用（Tauri 2）

`desktop/` 是 CC Remote 的桌面应用：把 `web/` 的前端装进原生窗口，可以保存多台宿主（和[独立网页前端](standalone.md)一样），并管理本机安装的 cc-remote 服务。安装包里没有宿主本身；宿主仍按[发布与安装](release.md)安装。

目前只在本地构建，还没接入 Release 工作流。

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
pnpm --dir web install
pnpm --dir desktop install
pnpm --dir desktop dev      # 启动 vite（5175 端口，--mode desktop）并运行应用
pnpm --dir desktop build    # 构建安装包（Windows：NSIS 与 MSI）
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
