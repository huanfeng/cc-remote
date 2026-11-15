# 首次使用指南

[返回首页](../README.md) · [English](getting-started.en.md)

目标：先在本机完成一个任务，再按需接入手机或另一台电脑。这里把**运行任务的电脑**称为宿主；项目目录、CLI 登录和后台服务都在宿主上。

## 1. 准备宿主

按需安装 [Claude Code](https://code.claude.com/docs) 或 [Codex CLI](https://developers.openai.com/codex/cli)，在将要运行 CC Remote 的系统账户下登录。只使用一个提供方时，不必安装另一个。

```sh
claude auth login  # Claude Code
codex login        # Codex
```

先在普通终端中确认对应 CLI 可以正常使用。后台复用宿主的 CLI 配置、登录和项目文件；它不会为你提供模型账号。

## 2. 安装并确认后台运行

### Windows x64（含客户端和托盘）

在普通 PowerShell 5.1 / 7 中运行：

```powershell
irm https://github.com/huanfeng/cc-remote/releases/latest/download/install-desktop.ps1 | iex
```

安装后会启动托盘：**悬停查看用量，单击打开客户端，右键打开菜单**。如需登录 Windows 时自启：

```powershell
& ([scriptblock]::Create((irm https://github.com/huanfeng/cc-remote/releases/latest/download/install-desktop.ps1))) -AutoStart
```

Windows ARM64 或只需要后台时，使用后台入口：

```powershell
irm https://github.com/huanfeng/cc-remote/releases/latest/download/install.ps1 | iex
```

### Linux / macOS

```sh
curl -fsSL https://github.com/huanfeng/cc-remote/releases/latest/download/install.sh | sh
```

### 确认安装成功

新开一个终端，运行：

```sh
ccr --version
ccr status
```

确认后台正在监听默认地址 `http://127.0.0.1:8787`。自定义过端口时，以状态中显示的地址为准。找不到命令或服务未运行时，先看本文末尾的[排查步骤](#遇到问题时)。

安装会下载最新正式版、校验 SHA256 并注册当前用户的后台服务。离线下载、自定义目录、跳过服务安装等参数见[发布与安装](release.md)。

## 3. 连接并开始任务

### Windows 客户端

点击托盘图标，或从开始菜单打开 **CC Remote**。在「本机服务」中连接，客户端从服务的数据目录读取令牌。连接其它电脑上的宿主时，需填写该宿主的地址和访问令牌。

### 浏览器连接与访问令牌

在**宿主电脑的浏览器**打开 <http://127.0.0.1:8787>，阅读安全说明后输入访问令牌。默认数据目录中的令牌可这样读取：

Windows（PowerShell）：

```powershell
Get-Content "$env:APPDATA\cc-remote\access-token"
```

Linux：

```sh
cat "${XDG_CONFIG_HOME:-$HOME/.config}/cc-remote/access-token"
```

macOS：

```sh
cat "$HOME/Library/Application Support/cc-remote/access-token"
```

自定义过数据目录时，使用 `ccr status` 显示的数据目录；通过环境变量设置令牌时，使用你配置的值。不要将令牌贴到 Issues、截图或分享链接中。

### 完成第一个任务

1. 点「新建」，选择 **Claude Code** 或 **Codex**。
2. 填写宿主上**已经存在**的项目目录。手机里的路径或另一台电脑的本地路径不能代替宿主路径。
3. 选择模型与权限，先使用默认设置即可。可以发送：`只读浏览这个项目，说明主要目录、技术栈和入口文件，先不要修改文件。`
4. 查看流式回复和工具过程，出现审批或提问时在界面中回答。

看到回复后，就完成了本机链路验证。会话在宿主上运行，关闭浏览器后仍可回来查看。

### Claude Code 终端接力

在项目目录运行 `ccr`，可以选择已有终端会话，没有则新建。按 **Ctrl+]** 断开后进程继续运行，在网页会话列表中打开同一个会话即可接着操作，再运行 `ccr` 接回。

```sh
cd your-project
ccr
ccr ls                # 浏览最近的终端会话
ccr --resume <会话ID> # 恢复 Claude Code 会话
```

`ccr` 与 `cc-remote` 完全等价。**这条终端接力路径用于 Claude Code**；Codex 请使用网页 / 客户端新建托管会话，或从「终端历史」导入并继续。导入原生历史前，应先停止原终端中的对应会话，避免并行操作同一个会话。

## 用手机或另一台电脑连接

### 同一局域网

默认监听只接受本机连接。在宿主上增加局域网监听：

```sh
ccr config --listen 127.0.0.1:8787,lan:8787
```

让手机与宿主连接同一可信局域网，在手机浏览器打开 `http://<宿主的局域网IP>:8787`，使用同一访问令牌连接。**不要在手机上填 `127.0.0.1`**，它指向手机自己。防火墙需允许该端口，访客 Wi-Fi / AP 隔离可能阻止设备互访。

局域网 HTTP 会明文传输令牌与会话内容。令牌相当于宿主上的代码执行权限，请只在可信网络使用。监听配置详见[配置说明](configuration.md)，安全边界见[安全说明](security.md)。

### 不在同一网络

先通过自己的 VPN、HTTPS 隧道或反向代理，让手机能够访问宿主。CC Remote 不提供网络穿透或 TLS 证书。使用反向代理时，终端功能还需要转发 WebSocket；配置见[安全说明](security.md#反向代理)。

### 公共网页客户端

[公共网页客户端](https://huanfeng.github.io/cc-remote/)适合在一个界面添加多台宿主；它仍然需要你的宿主在线、网络可达。公共 HTTPS 页面连接远程宿主时，宿主也需要 HTTPS，并允许来源 `https://huanfeng.github.io`。本机回环地址有例外；完整条件见[独立网页前端](standalone.md)。

不需要多宿主管理时，直接使用宿主自带的网页即可，无需另外部署前端。Windows 客户端也可保存多台宿主，连接方式见[桌面应用](desktop.md)。

## 更新与日常管理

```sh
ccr status       # 状态
ccr logs -f      # 跟踪日志
ccr restart      # 重启后台
ccr update       # 仅更新命令行与后台，默认等待会话空闲后重启
```

Windows 客户端和托盘请重新执行完整安装命令，保留账号、历史与配置。从托盘菜单退出运行中的客户端，再重新打开以使用新版。

后台与桌面客户端分开运行：退出托盘不停止后台采样；停止后台会影响其管理的会话。采样设置、多账号用量和历史见[订阅监控](subscription-monitor.md)。

## 遇到问题时

| 现象 | 先检查 |
| --- | --- |
| `ccr` 找不到 | 新开普通终端；确认安装目录已加入用户 PATH |
| 本机网页打不开 | `ccr status`；未运行时执行 `ccr start`，再看 `ccr logs -f` |
| 令牌无效或找不到 | 确认地址与数据目录属于同一宿主；自定义目录以状态输出为准 |
| 手机连不上 | 先确认宿主本机可访问，再检查 `lan:8787`、局域网 IP、防火墙和 Wi-Fi 隔离 |
| 公共网页能开，但宿主连接失败 | 检查宿主 HTTPS、网络可达性及允许的网页来源，见[独立网页](standalone.md) |
| 原生 CLI 能用，服务中报错或返回 403 | 确认服务的系统账户、CLI 路径与代理；服务不加载 shell 配置 |
| Windows 中服务看不到原来的数据 | 避免在 MSIX 应用的内置终端中安装或管理服务，使用普通 PowerShell / cmd，见[配置说明](configuration.md) |
| 终端会话没有网页提问 / 计划卡片 | 检查 Claude Code Hook 是否被禁用或被组织策略限制，见[使用说明](usage.md) |

如果代理仅写在 shell 配置里，可在已设好代理的普通终端执行 `ccr config --capture-env`，或按[配置说明](configuration.md)显式设置服务环境。

继续学习：[使用说明](usage.md) · [Codex](codex.md) · [供应商配置](supplier-profiles.md) · [订阅监控](subscription-monitor.md) · [返回首页](../README.md)
