# 配置

## 宿主命令行参数

直接运行 `cc-remote [参数]` 启动宿主（Web/API）。服务模式下这些参数由 `cc-remote service install/config` 保存和传入。

| 参数 | 默认值 | 用途 |
| --- | --- | --- |
| `--listen` | `127.0.0.1:8787` | 监听地址；可重复或逗号分隔，支持 `lan:PORT`、`all:PORT`、`localhost:PORT`（见[监听地址](#监听地址)） |
| `--data-dir` | 见[数据目录](#数据目录) | SQLite、令牌、附件、日志和宿主锁所在目录 |
| `--claude` | `claude` | Claude Code 可执行文件路径 |
| `--claude-config-dir` | `CLAUDE_CONFIG_DIR` 或 `~/.claude` | 原生历史发现和实际执行使用的 Claude 配置目录 |
| `--log-file` | 空（仅 stderr） | 同时写入日志文件，10 MiB 轮转并保留一个 `.1` 备份；Go 运行时崩溃信息也写入该文件 |
| `--allow-insecure-remote` | 关 | 允许 `all:PORT` / `0.0.0.0` / `::` 这类绑定所有网卡的地址 |
| `--lang` | `auto` | 提示信息和 Web 界面的默认语言：`auto`（跟随系统）、`zh` 或 `en`，见[界面语言](#界面语言) |
| `--allow-origin` | 空 | 允许访问此宿主的[独立网页前端](standalone.md)来源，如 `https://ccr.example.com`；可重复或逗号分隔。后台服务用 `service config --allow-origin` 设置（立即生效） |

其它子命令：`service`、`alias`、`update`、`version`、`attach`、`ccr`，`cc-remote help` 和 `cc-remote service --help` 查看用法。

一个数据目录同时只允许一个宿主运行。重启宿主会把仍在运行的轮次标记为中断、使旧审批失效，不会自动重放命令。

## 数据目录

优先级：`--data-dir` > 环境变量 `CC_REMOTE_DATA_DIR` > 默认目录。

| 系统 | 默认目录 |
| --- | --- |
| Windows | `%APPDATA%\cc-remote` |
| Linux | `${XDG_CONFIG_HOME:-~/.config}/cc-remote` |
| macOS | `~/Library/Application Support/cc-remote` |

目录内容：

| 文件 | 内容 |
| --- | --- |
| `cc-remote.sqlite`（及 `-wal`/`-shm`） | 会话、消息事件、审批记录 |
| `access-token` | 访问令牌（首次启动生成，权限 0600） |
| `preferences.json` | CC Remote 默认的模型、推理强度、权限模式 |
| `identity.json` | 主机名称与颜色（见[主机名称与颜色](#主机名称与颜色)） |
| `service.json` | 服务配置（监听地址、允许的网页前端 `allowOrigins`、Claude 路径、界面语言 `language`、服务环境变量等） |
| `uploads/<会话 ID>/` | 上传的附件 |
| `host.log` | 服务模式日志（10 MiB 轮转） |

此目录应只允许你自己的账户访问。旧版放在项目内 `./data` 的数据可以继续用 `--data-dir ./data` 使用。相同端口上的宿主必须使用同一目录的令牌，目录不匹配时会明确报错，不会接入其它宿主。

## 环境变量

| 变量 | 作用 |
| --- | --- |
| `CC_REMOTE_DATA_DIR` | 默认数据目录 |
| `CC_REMOTE_TOKEN` | 指定访问令牌（至少 16 个字符），代替 `access-token` 文件 |
| `CLAUDE_CONFIG_DIR` | Claude 配置目录（同 Claude Code 本身） |
| `CC_REMOTE_ALLOW_ORIGINS` | 同 `--allow-origin`，逗号分隔，与 `service.json` 的 `allowOrigins` 合并；后台服务建议改用 `service config --allow-origin` |
| `CC_REMOTE_LANG` | 界面语言 `zh` / `en`（`auto` 表示跟随系统），优先于 `service.json` 的设置；安装脚本把它作为 `--lang` 的默认值 |
| `CC_REMOTE_REPO` | 安装脚本和 `cc-remote update` 使用的仓库，默认 `huanfeng/cc-remote` |
| `CC_REMOTE_API_URL` | 安装脚本使用的 GitHub API 地址，默认 `https://api.github.com` |
| `GITHUB_TOKEN` / `GH_TOKEN` | 安装 / 更新时使用的 GitHub 令牌（可选） |

其它 `CC_REMOTE_*` 变量（如 `CC_REMOTE_BRIDGE_URL`、`CC_REMOTE_ASK_FILE`）由启动器内部传给子进程，无需设置。

## 监听地址

`--listen` 可重复或逗号分隔，每个地址单独监听，任一地址失败即报错退出：

```sh
cc-remote --listen 127.0.0.1:8787 --listen 192.168.1.10:8787
cc-remote --listen 127.0.0.1:8787,lan:8787
```

| 写法 | 含义 |
| --- | --- |
| `127.0.0.1:8787` / `IP:PORT` | 指定地址 |
| `localhost:PORT` | 等同 `127.0.0.1:PORT` |
| `lan:PORT` | 启动时解析为本机所有私有 IPv4 地址（10/8、172.16/12、192.168/16），日志打印实际绑定列表；DHCP 换 IP 后需重启（服务重启会重新解析） |
| `all:PORT` | 等同 `0.0.0.0:PORT`，必须加 `--allow-insecure-remote` |

- 重复地址自动去重；已有同端口的 `0.0.0.0` 时，其它同端口地址跳过。
- 回环地址始终可达：解析结果里既没有回环地址也没有 `0.0.0.0`/`::` 时，自动追加 `127.0.0.1:<第一个地址的端口>` 并写日志。`service` 子命令、`ccr` 等本机工具只连回环地址，访问令牌不会发往其它主机。
- 监听非回环地址时启动日志会打印安全警告（宿主日志统一为英文），`service install/config/status` 也会按界面语言提示，参见 [security.md](security.md)。

## 界面语言

命令行输出（`cc-remote`、`ccr`、`service`、`update` 等）和 Web 界面的默认语言支持简体中文和英文：

- 宿主：`--lang auto|zh|en`。`auto`（默认）跟随系统语言：Windows 取用户界面语言，Linux / macOS 依次取 `LANGUAGE`、`LC_ALL`、`LC_MESSAGES`、`LANG`；中文环境为中文，其它为英文。
- 后台服务：`service install --lang zh` 或 `service config --lang en` 保存到 `service.json` 的 `language` 字段（`auto` 清除该字段），宿主每次启动时读取。重装 / `update` 未传 `--lang` 时沿用原设置。`auto` 在安装、`service config`、`update` 时按当前终端的语言确定（launchd / systemd 启动的宿主拿不到你的 `LANG`），之后改了系统语言，再执行一次 `service config --lang auto` 即可。
- 手动运行的命令和手动启动的宿主（未传 `--lang`）：依次取环境变量 `CC_REMOTE_LANG`、已安装服务的 `language`、系统语言。
- Web 界面：默认使用宿主的语言，每个浏览器可以在网页设置中单独选择语言。
- 宿主日志（`host.log`）、Claude Code 看到的钩子 / MCP 文本不翻译，保持英文。

```sh
cc-remote service config --lang zh      # 改为中文（宿主空闲时自动重启生效）
cc-remote service config --lang auto    # 恢复跟随系统
CC_REMOTE_LANG=en ccr ls                # 本次命令用英文
```

## 主机名称与颜色

部署了多台宿主时，用名称和颜色在浏览器里区分它们：名称显示在标签页标题（`pdev · CC Remote`）、侧栏和登录页，安装到手机桌面时也作为应用名；颜色用于标签页图标和侧栏标志。

- 默认：名称为机器名（去掉域名部分），颜色按名称从调色板中挑选，所以不设置时各台宿主通常也不同色。
- 网页：「设置 → 此主机」修改，立即生效。
- 命令行：`cc-remote service config --name pdev --color teal`（`service install` 同样接受），无需重启服务，已打开的页面刷新后生效。`--color auto`、`--name ""` 恢复默认。
- 颜色：`green`、`teal`、`blue`、`indigo`、`purple`、`pink`、`red`、`orange`、`amber`、`slate`，或 `#rrggbb`。
- 保存在数据目录的 `identity.json`。登录前的页面就会显示名称（便于确认连的是哪台），因此能访问该端口的人可以看到它。

## 独立网页前端

部署了多台宿主时，可以用一个单独部署的静态前端连接、切换它们。可以直接使用公共实例 <https://huanfeng.github.io/cc-remote/>，也可以部署到 Vercel、Cloudflare Pages 或任意静态网站。前端和宿主都必须通过 HTTPS 访问，并且每台宿主都要允许前端的来源：

```sh
cc-remote service config --allow-origin https://ccr.example.com   # 后台服务：立即生效，整体替换列表（多个用逗号分隔，"" 清空）
cc-remote --allow-origin https://ccr.example.com                  # 前台运行
```

部署步骤、HTTPS 要求，以及内网穿透（302 跳转、端口会变化的文本地址）的用法见 [standalone.md](standalone.md)。

## 后台服务

`cc-remote service install` 把宿主注册为当前用户的后台服务，登录后自动启动，使用当前账户的 Claude 登录和 `~/.claude`：

| 系统 | 机制 | 可执行文件副本 |
| --- | --- | --- |
| Windows | 计划任务「CC Remote」（登录触发、当前用户、无控制台窗口，监督进程崩溃自动重启） | `%LOCALAPPDATA%\cc-remote\bin\cc-remote.exe` |
| Linux | systemd 用户单元 `~/.config/systemd/user/cc-remote.service` | `${XDG_DATA_HOME:-~/.local/share}/cc-remote/bin/cc-remote` |
| macOS | LaunchAgent `~/Library/LaunchAgents/com.cc-remote.host.plist` | `~/Library/Application Support/cc-remote/bin/cc-remote` |

安装时复制当前可执行文件；更新只需用新版本再执行一次 `service install`（复制、更新服务定义，宿主空闲时自动重启到新版本），`cc-remote update` 和安装脚本也是这样做的。

```sh
cc-remote service install                                   # 默认 127.0.0.1:8787，并启动
cc-remote service install --listen 127.0.0.1:8787 --listen lan:8787 --alias
cc-remote service config                                    # 查看当前配置
cc-remote service config --listen 127.0.0.1:8787,lan:8787   # 修改监听地址（无需重启）
cc-remote service status                                    # --json 输出 JSON；未运行时退出码 3
cc-remote service logs -n 200 -f
cc-remote service stop | start | restart
cc-remote service uninstall                                 # 保留数据目录；--purge-binary 同时删除副本
```

- `install` 参数：`--listen`（可重复）、`--data-dir`、`--claude`（安装时解析为绝对路径）、`--claude-config-dir`、`--allow-insecure-remote`、`--lang`、`--env` / `--capture-env` / `--no-capture-env`、`--no-start`、`--alias`、`--force`（立即重启运行中的宿主）、`--dry-run`（只打印将执行的操作、任务 XML / unit / plist；环境变量值打码）。
- 重复安装（包括 `update` 和安装脚本）未传 `--data-dir` 时，沿用安装目录中 `service.json` 副本记录的数据目录、监听地址、Claude 路径、Claude 配置目录、界面语言和服务环境变量；显式传入的参数覆盖原值。
- 配置保存在 `<数据目录>/service.json`，安装目录保留一份副本，`config`/`start`/`stop`/`status`/`logs`/`uninstall` 据此找到非默认数据目录。
- `stop` 先请求宿主优雅退出（不会被服务管理器重新拉起），15 秒未退出再强制结束。宿主异常退出时自动重启。
- 数据目录锁或端口被其它进程（通常是另一个 cc-remote）占用时，宿主以退出码 75 退出；Windows 监督进程连续 3 次遇到后停止重试并在 `host.log` 中说明原因。
- `all:` / `0.0.0.0` / `::` 需要 `--allow-insecure-remote`（保存在 `service.json`，可用 `service config --allow-insecure-remote=false` 撤销）。旧版本按 `0.0.0.0` 安装的服务在重装时保留原地址并自动记录该选项，同时打印警告。Windows 还会打印可选的防火墙放行命令（不会自动执行）。
- Linux / macOS 安装时把当前 `PATH` 写入服务环境，以便找到 `claude` / `node`（Claude Code 插件的 hook、MCP 服务常用 node 运行）。重新安装和 `cc-remote update` 会保留已安装服务 `PATH` 里的目录，从 `PATH` 较短的 shell（例如不加载登录配置的 ssh 命令）更新也不会丢掉 nvm 等目录；服务的 `PATH` 里找不到 `node` 时安装会给出警告，这时请在能找到 node 的登录 shell 中重新运行 `cc-remote service install`，或用 `cc-remote service config --env PATH=…` 指定。Linux 需要开机即启动（无需登录）时执行 `loginctl enable-linger $USER`。
- 日志：`<数据目录>/host.log`；Windows 监督进程把宿主 stderr 也追加到其中；macOS 的 `<数据目录>/launchd.log` 只接收宿主打开 `host.log` 之前的输出。
- `uninstall --purge-binary` 删除副本目录，并从用户 PATH 中移除该目录。

### 修改设置与重启

原则是尽量不打断正在进行的工作：

- `service config`：先保存到 `service.json`，再让运行中的宿主重新加载。监听地址（`--listen`、`--allow-insecure-remote`）立即生效，不重启；`--claude`、`--claude-config-dir`、`--lang`、`--env` 需要重启，宿主会等到没有会话在执行、等待回答或运行后台任务时自动重启，并列出正在忙碌的会话。主机名称与颜色本来就不需要重启。
- `service install`（以及 `update`、安装脚本）：宿主在运行时不停止它，只替换程序，等空闲后自动重启到新版本。
- 加 `--force`：立即重启，不等会话空闲（`service config --force`、`service install --force`、`update --force`）。`--no-restart` 只保存，宿主下次重启时生效。
- `service restart` 始终立即重启。
- 宿主不在运行时，各命令按原样注册并启动服务。

监听地址热更新时，新地址绑定失败（例如端口被占用）则报错并恢复原来的 `service.json`，本次的其它修改也不保存，其它地址不受影响（只有与新地址同端口、且一方是 `0.0.0.0` / `::` 的旧地址会先关闭，失败后重新绑定）。移除的地址约 1 秒后断开经它建立的连接。`ccr` 和本机钩子使用的回环地址（通常是第一个回环地址）不能在运行时移除，修改它会改为空闲重启。

重启由宿主以退出码 76 退出、服务管理器重新拉起完成。宿主每次启动都从 `service.json` 读取监听地址、Claude 路径、语言和环境变量（`auto` 语言按保存设置时终端的语言确定，记在 `autoLanguage`），因此 Windows 计划任务和 macOS LaunchAgent 在修改设置时不需要重新注册（重新注册会立即停止宿主；macOS 只重写 plist，不重新加载）；Linux 的 unit 文件随之更新并 `daemon-reload`，不影响运行中的宿主。宿主不是由服务启动的，或版本早于 v0.5、不能自动重启时，仍按原来的方式停止它并启动服务。重启前正在运行的终端会话记在 `restore.json`，重启后自动恢复，接入中的 `ccr` 会自动重新接入。

### 服务环境变量与代理

服务不加载 shell 配置（PowerShell `$PROFILE`、`~/.bashrc` 等），其中设置的代理不会生效，表现为服务中的会话报 `API Error: 403 Request not allowed`，而终端里直接运行 `claude` 正常。

- 首次 `install`（或尚无环境配置时）默认从当前进程捕获非空的 `HTTP_PROXY` / `HTTPS_PROXY` / `ALL_PROXY` / `NO_PROXY`（任意大小写），`--no-capture-env` 关闭；之后重装 / `update` 沿用已保存的环境。
- 其它变量只能用 `--env KEY=VALUE` 显式传入（可重复，`KEY=` 删除），不会自动捕获任何密钥。

```sh
cc-remote service config --env HTTPS_PROXY=http://127.0.0.1:7890 --env NO_PROXY=localhost,127.0.0.1
cc-remote service config --capture-env      # 在已设好代理的终端里重新捕获
cc-remote service config --env HTTPS_PROXY= # 删除
```

变量保存在 `service.json`（权限 0600）的 `env` 中，通过 `--env` 传入的密钥也以明文保存。Windows 监督进程每次启动宿主时读取它们，Linux / macOS 写入 unit 的 `Environment=` / plist 的 `EnvironmentVariables`；宿主启动时还会再从 `service.json` 设置一次，并清除用 `--env KEY=` 删除过的变量（记在 `envRemoved`），所以无需重新注册服务也能生效。删除过的变量即使由系统或用户环境提供也会被清除，直到再次用 `--env` 设置它。`service status` / `config` 的 `Env:` 行显示代理变量的值（URL 中的密码隐去），其它变量显示 `****`。修改后宿主空闲时自动重启生效。

### ccr 别名

`ccr` 启动或接入终端会话（等同 `cc-remote ccr`），不替换 `claude` 本身。安装脚本默认创建它，也可手动管理：

```sh
cc-remote alias install      # Windows：在副本目录写 ccr.cmd 并加入用户 PATH；Linux/macOS：写 ~/.local/bin/ccr，并链接 ~/.local/bin/cc-remote
cc-remote alias status
cc-remote alias uninstall
```

别名指向已安装的服务副本，并带上服务的数据目录和地址；未安装服务时指向当前可执行文件并给出警告。Windows 修改用户 PATH 后需打开新终端，cmd 和 PowerShell 均可运行 `ccr`。Linux / macOS 的程序本体在 `~/.local/share/cc-remote/bin`（macOS 为 `~/Library/Application Support/cc-remote/bin`），因此同时在 `~/.local/bin` 创建 `cc-remote` 符号链接，让 `cc-remote` 命令与 `ccr` 一样可直接使用；已有同名普通文件时不覆盖。若 `~/.local/bin` 不在 PATH 会提示添加。把可执行文件复制或链接为 `ccr` 效果相同。

## ccr 与 Claude Code 参数

- `ccr` 自己的参数：`--host`（本机宿主地址，默认 `http://127.0.0.1:8787`）、`--data-dir`、`--new`、`--title`。
- `--model`、`--effort`、`--permission-mode` 成为终端会话的设置（网页可改），未给出时用 CC Remote 默认值。
- `-r` / `--resume [ID 或关键词]`、`-c` / `--continue` 以终端会话继续 Claude Code 会话。
- 其它参数按 Claude Code 的规则读取（哪些参数带值、`--add-dir` 等可带多个值）并随会话保存，每次启动（包括宿主重启后用 `--resume` 恢复）都传给 Claude Code；`-n` / `--name` 同时作为会话标题。
- 宿主自己设置或与终端会话冲突的参数会被拒绝：`--settings`、`--session-id`、`-p` / `--print`、`--output-format`、`--input-format`、`--bare`、`--safe-mode`、`--no-session-persistence`、`--fork-session`、`--bg`、`--cloud`、`--teleport` 等。
- 带了 Claude Code 参数或用了 `--` 时总是新建会话，其余文字作为第一条消息发送（不保存，恢复会话时不会重发）。

## 权限模式、模型与推理强度

三级生效：**会话覆盖 → CC Remote 默认（`preferences.json`）→ Claude 自身配置**。

- 前两级以 `--permission-mode`、`--model`、`--effort` 启动参数传给 `claude`，**不修改** `~/.claude/settings.json`。
- 支持全部 7 种权限模式。`bypassPermissions`（跳过所有审批）、`dontAsk`、`auto` 需要二次确认；该确认只是防误点，不是安全边界——任何持有访问令牌的人都能设置这些模式。用户已有的 allow/deny 规则仍然生效。
- 托管会话的修改下一轮生效；终端会话在下次启动 Claude Code 时生效，运行中可在终端用 `/model`、`/effort` 或 Shift+Tab 即时修改。
- `preferences.json` 损坏时，托管会话与终端会话都会记录警告并忽略默认设置继续运行。
- 模型名只接受字母或数字开头、由字母数字和 `._:/[]@-` 组成、最长 200 字符的值。留空或 `inherit` 表示清除覆盖、不传 `--model`；`default` 是 Claude Code 自身的模型别名，会原样作为 `--model default` 传递。

### 模型列表

`GET /api/models` 汇总：Claude Code 官方别名、settings 的 `model` / `availableModels` / 模型类环境变量、本机原生会话最近使用的模型；配置了 `ANTHROPIC_API_KEY` / `ANTHROPIC_AUTH_TOKEN` 时还会请求 `/v1/models`（密钥与 `ANTHROPIC_BASE_URL` 只从同一来源读取：宿主进程环境变量，或用户 settings 的 `env`；非本机地址必须为 https，不跟随重定向，5 秒超时）。结果缓存 10 分钟。设置了 `availableModels` 时只列出允许项与 default。

### 写入 Claude 用户配置

全局设置中的「Claude 用户配置」可选写入 `~/.claude/settings.json`：只原位改写 `model`、`effortLevel`、`permissions.defaultMode`，保留其它字段、键顺序、缩进与换行风格；每次宿主运行首次写入前备份为 `settings.json.cc-remote.bak`，并检测版本冲突。这会影响该账户下所有 Claude Code 使用，项目或组织设置仍可能覆盖它。
