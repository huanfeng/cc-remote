# CC Remote

Go 宿主 + React/TypeScript PWA + SQLite 的 Claude Code 远程界面。直接运行本机的 Claude Code CLI，使用结构化消息和审批桥接，无需终端模拟器。

## 当前功能

开发任务、完成状态和续接边界见 [docs/tasks.md](docs/tasks.md)。

- 左侧列表管理会话：打开过的会话显示「已缓存」，切换保留消息、草稿、截图、滚动位置、工具展开状态和未提交的回答；点击「×」释放缓存或刷新网页会清除未发送草稿，已发送消息仍由宿主保存，宿主任务继续执行。
- 紧凑布局与可折叠侧栏；桌面端拖动侧栏右边缘调整宽度（200–480px，不超过窗口 40%，双击恢复默认，方向键每次 16px，宽度记在本机）；Enter / Ctrl+Enter 发送方式可选，Shift+Enter 换行，兼容中文输入法。
- 界面分区：顶栏只放当前会话——标题、项目目录、模型 · 推理强度 · 权限模式只读标签（继承值显示为「默认(…)」，`bypassPermissions` 标红、`dontAsk`/`auto` 标黄）、状态、刷新和「会话设置」。会话设置对话框可修改模型、推理强度、权限模式（保存/取消、全部恢复默认），并显示会话 ID、Claude 会话 ID、目录、模式、创建时间和用量摘要；共享终端会话只读。侧栏顶部显示宿主版本、Claude Code 连接状态和退出按钮；全局「设置」（分区：CC Remote 默认、Claude 用户配置）与「服务状态」并排在侧栏底部，手机抽屉同样可用。
- 删除会话：侧栏会话项的删除图标（桌面悬停显示，手机常显）或会话设置中的「删除…」，确认后删除 CC Remote 中该会话的消息/事件、审批记录、共享终端同步记录和上传附件；**不删除** `~/.claude` 下的原生 JSONL，仍可在「终端历史」中查看或恢复。执行中的托管会话需勾选「先停止再删除」；在线的共享终端须先退出终端。
- 令牌记在当前站点 localStorage，刷新及重新打开网页无需重登；退出清除。
- 附件：点击「＋ 附件」、粘贴或拖放到输入框，支持任意类型文件，单个最多 20 MiB，每条消息合计最多 8 个（图片与文件合计）。PNG/JPEG/GIF 仍校验内容（最多 2500 万像素）并显示缩略图，其它文件原样保存为文件卡片（名称、大小，可下载）。文件保存在宿主数据目录 `uploads/<会话 ID>/`，文件名清理为无路径、无控制字符、最长 120 字节并保留扩展名；消息会附上每个附件的宿主绝对路径和原文件名，提示 Claude 用 Read 工具读取（支持文本、PDF、图片、Notebook），托管与共享终端会话相同，可能需要一次读取审批。
- 权限模式、模型、推理强度三级生效：会话覆盖 → CC Remote 默认（数据目录 `preferences.json`）→ Claude 自身配置。前两级以 `--permission-mode`、`--model`、`--effort` 启动参数传给 `claude`，**不修改** `~/.claude/settings.json`；支持全部 7 种权限模式，`bypassPermissions`（跳过所有审批）、`dontAsk`、`auto` 需二次确认。该确认（API 字段 `confirmDangerous`）只是防误点，不是安全边界：任何持有访问令牌的人都能设置这些模式。托管会话改动下一轮生效；共享终端由运行中的进程持有，需在终端执行 `/model`、`/effort` 或 Shift+Tab。`cc-remote -- claude` 启动共享终端时若危险模式来自网页默认设置（命令行未显式传权限参数），会先在终端打印醒目警告，可传 `--permission-mode default` 覆盖；`preferences.json` 损坏时托管会话与共享终端都会给出警告并忽略默认设置继续运行。
- 模型名只接受字母或数字开头、由字母数字和 `._:/[]@-` 组成、最长 200 字符的值（防止被当作命令行参数）。留空 / `inherit` 表示清除覆盖、不传 `--model`；`default` 是 Claude Code 自身的模型别名，会原样作为 `--model default` 传递。
- 模型选择器（设置、新建会话、会话设置共用）可输入任意模型 ID，列表由 `GET /api/models` 动态汇总：Claude Code 官方别名、settings 的 `model`/`availableModels`/模型类环境变量、本机原生会话最近使用的模型、配置了 `ANTHROPIC_API_KEY`/`ANTHROPIC_AUTH_TOKEN` 时的 `/v1/models`（密钥与 `ANTHROPIC_BASE_URL` 只从同一来源读取：宿主进程环境变量，或用户 settings 的 `env`；非本机地址必须为 https，不跟随重定向，5 秒超时）；缓存 10 分钟，可手动刷新。设置了 `availableModels` 时只列允许项与 default。
- 可选「写入 Claude 用户配置」：只原位改写 `model`、`effortLevel`、`permissions.defaultMode`，保留键顺序、缩进与 CRLF/LF；每次宿主运行首次写入前备份为 `settings.json.cc-remote.bak`。会影响该账户下所有 Claude Code 使用。
- 网页 `/help`、`/status`、`/config`、`/permissions`、`/model`、`/effort`、`/permission-mode`；`/model` 不带参数列出可用模型，带参数设置当前托管会话（加 `--default` 改 CC Remote 默认，`inherit` 恢复默认），`/effort` 同理；托管会话额外支持 `/clear` 和 `/stop`。完整原生控制命令仍在终端执行，网页会明确提示。
- PC 侧栏和手机抽屉使用同一会话列表，显示执行中、等待操作、已完成及失败等状态，后台缓存会话持续刷新；共享模式显示终端连接状态与待审批提示，尚无完整原生运行状态接口。
- 发现项目及用户级 `skills/*/SKILL.md`、旧式 `commands/**/*.md`，支持静态 `$ARGUMENTS` 展开；动态 shell、位置参数与原生技能 hooks/执行选项暂不支持，需在终端执行。

### 最简共享终端启动

将可执行文件放到 PATH 后，在项目目录运行：

```sh
cc-remote -- claude
cc-remote --data-dir /your/private/cc-remote -- claude --model sonnet
cc-remote -- claude --resume <会话UUID>
```

首次自动启动本地宿主，再打开原生 Claude；首次 Channel 确认仍在终端操作。默认数据目录为 Windows `%APPDATA%\cc-remote`、Linux `${XDG_CONFIG_HOME:-~/.config}/cc-remote`；`CC_REMOTE_DATA_DIR` 可更改默认目录，`--data-dir` 优先。SQLite、令牌、上传图片和后台日志 `host.log` 都放在该目录。此目录应只允许自己的账户访问。

已有旧版项目内 `./data` 可以继续使用 `cc-remote --data-dir ./data -- claude`。相同端口上的宿主必须使用同一目录的令牌；目录不匹配时会明确报错，不接入其它宿主。`--host` 指定本机桥接地址；远端网页继续访问你配置的 IP 或代理。

启动器读取 CC Remote 默认设置：命令行未给出 `--permission-mode`/`--dangerously-skip-permissions`/`--allow-dangerously-skip-permissions`、`--model`、`--effort` 时自动追加对应默认值，显式参数始终优先。原生 `--model`、`--permission-mode`、精确 UUID 的 `--resume/-r` 和 `--session-id` 可透传。为保证共享绑定，暂不接受 `--continue`、`--fork-session`、`-p`、覆盖 `--settings/--mcp-config/--channels`。旧的 `terminal` 子命令继续支持。

- 原生终端共享会话：终端保持运行，网页通过 Claude Channels 向同一个 CC 进程发送消息、接收回复和工具审批。
- 多个终端各自注册，共享会话自动出现在「会话」列表；显示在线/离线状态，沿用原会话标题。
- 创建、切换多个会话，指定各自的宿主工作目录。
- 流式回复、Markdown、代码块、工具调用和结果。
- 会话之间可以并行，同一会话一次执行一轮；支持停止当前轮次。
- 权限审批、AskUserQuestion 选项和计划确认转换成 Web 操作。
- SQLite 保存消息；刷新页面或关闭浏览器不会停止任务。
- 浏览已有原生 Claude Code 会话历史；停止原进程后可导入继续。
- 访问令牌认证、移动端布局、PWA manifest 和静态资源缓存。
- 构建后将 Web 资源嵌入宿主可执行文件。

## 环境要求

- Go 1.26 或更新版本。
- Node.js 22 / pnpm（版本见 `web/package.json` 的 `packageManager`，可用 `corepack enable` 安装），用于构建前端；运行已构建宿主无需 Node。
- Windows 或 Linux；安装近期版本的 Claude Code，并在宿主执行 `claude auth login`。
- 本版使用当前 CLI 的 exec-form hooks 和 permission host。实际集成在 Claude Code 2.1.299 上验证；旧版本不保证兼容。

## 安装与更新

从 GitHub Release 安装预编译版本（仓库私有，需要已 `gh auth login` 或设置 `GITHUB_TOKEN`）。重复执行即为更新：

```powershell
# Windows
gh api repos/huanfeng/cc-remote/contents/scripts/install.ps1 -H "Accept: application/vnd.github.raw" | Out-String | Invoke-Expression
```

```sh
# Linux / macOS
gh api repos/huanfeng/cc-remote/contents/scripts/install.sh -H "Accept: application/vnd.github.raw" | sh
```

已安装后也可执行 `cc-remote update` / `cc-remote update --check`。参数、令牌方式、发版流程、卸载与故障排查见 [docs/release.md](docs/release.md)。

## 构建与运行

在项目根目录执行：

```sh
pnpm --dir web install --frozen-lockfile
pnpm --dir web run build
go build -o cc-remote ./cmd/cc-remote
```

Windows 构建时使用：

```powershell
go build -o cc-remote.exe ./cmd/cc-remote
.\cc-remote.exe
```

Linux：

```sh
./cc-remote
```

打开 http://127.0.0.1:8787 。首次启动在数据目录生成 `access-token`，将文件里的令牌输入登录页面。令牌保存在当前站点的 localStorage 中，不放进 URL。

Windows 读取令牌：

```powershell
Get-Content "$env:APPDATA\cc-remote\access-token"
```

Linux：

```sh
cat "${XDG_CONFIG_HOME:-$HOME/.config}/cc-remote/access-token"
```

如果希望通过局域网 IP 访问，启动时设置监听地址（`lan:PORT` = 本机所有私有 IPv4 地址）：

```powershell
.\cc-remote.exe --listen lan:8787
```

```sh
./cc-remote --listen lan:8787
```

> **安全提示**：Web/API 是明文 HTTP，持有访问令牌即可以你的身份在宿主上执行代码。只在可信局域网使用，或放在 HTTPS 反向代理 / 隧道之后。宿主启动时会对每个非回环地址打印中英文警告。绑定所有网卡（`all:PORT`、`0.0.0.0`、`::`）必须显式加 `--allow-insecure-remote`。

也可以通过 `CC_REMOTE_TOKEN` 环境变量设置至少 16 字符的访问令牌。令牌持有者可以读取会话，并通过 CC 操作宿主工作目录。远程连接的加密由你的 HTTPS 反向代理或私有网络提供。通过 HTTP 远程 IP 可以使用 Web 界面；PWA 的安装与 Service Worker 通常要求 HTTPS（localhost 除外）。

CLI 参数：

| 参数 | 默认值 | 用途 |
| --- | --- | --- |
| `--listen` | `127.0.0.1:8787` | Web/API 监听地址；可重复或逗号分隔，支持 `lan:PORT`、`all:PORT`、`localhost:PORT`（见下文「多地址监听」） |
| `--log-file` | 空（仅 stderr） | 同时写入日志文件，10 MiB 轮转并保留一个 `.1` 备份；Go 运行时崩溃信息也写入该文件。由服务管理器启动时只写文件、不再复制到 stderr |
| `--allow-insecure-remote` | 关 | 允许 `all:PORT` / `0.0.0.0` / `::` 这类绑定所有网卡的地址（明文 HTTP） |
| `--data-dir` | 用户配置目录下的 `cc-remote` | SQLite、令牌、图片和宿主锁目录；可由 `CC_REMOTE_DATA_DIR` 改变默认值 |
| `--claude` | `claude` | Claude Code 可执行文件路径 |
| `--claude-config-dir` | `CLAUDE_CONFIG_DIR` 或 `~/.claude` | 原生历史发现和实际执行使用的同一配置目录 |

一个数据目录同时只允许一个宿主运行。重启宿主会把仍在运行的轮次标记为中断，并使旧审批失效，不会自动重放命令。

### 多地址监听

`--listen` 可重复或逗号分隔，每个地址单独监听，任一地址失败即报错退出：

```sh
cc-remote --listen 127.0.0.1:8787 --listen 192.168.1.10:8787
cc-remote --listen 127.0.0.1:8787,lan:8787
```

- `lan:PORT`：启动时解析为本机所有私有 IPv4 地址（10/8、172.16/12、192.168/16），日志打印实际绑定列表。DHCP 换 IP 后需重启宿主（服务重启时会重新解析）。
- `all:PORT` 等同 `0.0.0.0:PORT`，需要 `--allow-insecure-remote`；`localhost:PORT` 等同 `127.0.0.1:PORT`。
- 重复地址自动去重；已有同端口的 `0.0.0.0` 时，其它同端口地址跳过。
- 回环地址始终可达：解析结果里既没有回环地址也没有 `0.0.0.0`/`::` 时，自动追加第一个地址端口上的 `127.0.0.1:PORT` 并写日志。`service` 子命令、`ccr` 和本机工具只连回环地址，访问令牌不会发往其它主机。
- 终端桥接优先使用回环监听地址，其次 `0.0.0.0` 对应的 `127.0.0.1`。

## 系统服务与 ccr 别名

`cc-remote service` 把宿主注册为当前用户的后台服务，登录后自动启动，使用当前账户的 `claude auth login` 和 `~/.claude`：

| 系统 | 机制 | 可执行文件副本 |
| --- | --- | --- |
| Windows | 计划任务「CC Remote」（登录触发、当前用户、隐藏、无控制台窗口） | `%LOCALAPPDATA%\cc-remote\bin\cc-remote.exe` |
| Linux | systemd 用户单元 `~/.config/systemd/user/cc-remote.service` | `${XDG_DATA_HOME:-~/.local/share}/cc-remote/bin/cc-remote` |
| macOS | LaunchAgent `~/Library/LaunchAgents/com.cc-remote.host.plist` | `~/Library/Application Support/cc-remote/bin/cc-remote` |

安装时复制当前可执行文件，此后仓库里的 `cc-remote.exe` 可以随时重新构建；更新服务只需用新构建再执行一次 `service install`（自动停止、复制、重新注册、启动）。

```sh
cc-remote service install                                   # 默认 127.0.0.1:8787，并启动
cc-remote service install --listen 127.0.0.1:8787 --listen lan:8787 --alias
cc-remote service config                                    # 查看当前配置
cc-remote service config --listen 127.0.0.1:8787,lan:8787   # 修改监听地址并重启服务
cc-remote service status                                    # --json 输出 JSON；未运行时退出码 3
cc-remote service logs -n 200 -f
cc-remote service stop | start | restart
cc-remote service uninstall                                 # 保留数据目录；--purge-binary 同时删除副本
```

- `install` 参数：`--listen`（可重复）、`--data-dir`、`--claude`（安装时解析为绝对路径）、`--claude-config-dir`、`--allow-insecure-remote`、`--env`/`--capture-env`/`--no-capture-env`（见下文服务环境变量）、`--no-start`、`--alias`、`--dry-run`（只打印将执行的操作、任务 XML/unit/plist 和注册表改动；环境变量值打码）。
- 重复安装（包括 `cc-remote update` 和安装脚本）未传 `--data-dir` 时，先读安装目录里的 `service.json` 副本，沿用其中的数据目录、监听地址、`claude`、Claude 配置目录与服务环境变量；显式传入的参数覆盖原值。
- 配置保存在 `<数据目录>/service.json`，并在安装目录保留一份副本，`config`/`start`/`stop`/`status`/`logs`/`uninstall` 据此找到非默认数据目录；也可显式传 `--data-dir`。安装目录无法确定（无 `LOCALAPPDATA`/`HOME`）时直接报错，不会退回临时目录。
- 日志写入 `<数据目录>/host.log`，10 MiB 轮转；服务模式下只写该文件，Go 运行时崩溃信息也写入其中。
- `stop` 先通过 `POST /api/admin/shutdown` 优雅退出（退出码 0，不会被服务管理器重新拉起），15 秒未退出再由服务管理器强制结束；服务本就未运行时 `stop` 不报错。宿主异常退出时自动重启。
- 数据目录锁或监听端口被其它进程（通常是另一个 cc-remote）占用时，宿主以退出码 75 退出；Windows 监督进程连续 3 次遇到该退出码后停止重试并在 `host.log` 中说明原因。
- 监听非回环地址时会打印中英文安全警告；`all:`/`0.0.0.0`/`::` 需要 `--allow-insecure-remote`（保存在 `service.json`，可用 `service config --allow-insecure-remote=false` 撤销）。旧版本已按 `0.0.0.0` 安装的服务在重新安装/`update` 时保留原地址并自动记录该选项，同时打印警告。Windows 还会打印可选的防火墙放行命令（不会自动执行）。
- Linux 和 macOS 安装时把当前 `PATH` 写入服务环境，以便找到 `claude`/`node`。Linux 需要开机即启动（无需登录）时执行 `loginctl enable-linger $USER`。macOS 的 `<数据目录>/launchd.log` 只接收宿主打开 `host.log` 之前的输出（如参数错误），体积很小。
- Windows 监督进程把宿主的 stderr（启动失败等）也追加到 `host.log`。
- 服务环境变量：服务不加载 shell 配置（PowerShell `$PROFILE`、`~/.bashrc` 等），其中设置的代理不会生效。首次 `install`（或已有安装尚无环境配置时）默认从当前进程捕获非空的 `HTTP_PROXY`/`HTTPS_PROXY`/`ALL_PROXY`/`NO_PROXY`（任意大小写，保留原名；Windows 不区分大小写去重），`--no-capture-env` 关闭；之后重复安装/`update` 沿用已保存的环境。其它变量只能显式传 `--env KEY=VALUE`（可重复，`KEY=` 删除），不会自动捕获任何密钥。`install`/`config` 均支持 `--env`、`--capture-env`、`--no-capture-env`：

  ```sh
  cc-remote service config --env HTTPS_PROXY=http://127.0.0.1:7890 --env NO_PROXY=localhost,127.0.0.1
  cc-remote service config --capture-env      # 在已设好代理的终端里重新捕获
  cc-remote service config --env HTTPS_PROXY= # 删除
  ```

  变量保存在 `service.json`（权限 0600）的 `env` 中，通过 `--env` 传入的密钥也以明文存于此文件。Windows 监督进程启动时读取 `<数据目录>\service.json`，把这些变量合并（不区分大小写覆盖）进宿主及其启动的 `claude` 的环境；Linux 写入 unit 的 `Environment=`，macOS 写入 plist 的 `EnvironmentVariables`。`service status`/`config` 的 `Env:` 行显示代理变量的值（URL 中的密码隐去），其它变量只显示 `****`。
- 故障排查：服务中的会话报 `API Error: 403 Request not allowed`，而终端里直接运行 `claude` 正常，通常是代理只在 shell 配置中设置、服务直连被拒；用 `service config --env HTTPS_PROXY=...` 或 `--capture-env` 设置后服务自动重启。
- `uninstall --purge-binary` 删除副本目录，并从用户 PATH 中移除该目录（`ccr.cmd` 所在处）。PATH 比较会展开 `%VAR%`/`$VAR`，Windows 下不区分大小写。

`ccr` 是 `cc-remote -- claude` 的简写，不替换 `claude` 本身：

```sh
cc-remote alias install      # Windows：在副本目录写 ccr.cmd 并加入用户 PATH；Linux/macOS：写 ~/.local/bin/ccr
cc-remote alias status
cc-remote alias uninstall
ccr --model sonnet           # 等同 cc-remote -- claude --model sonnet
```

别名指向已安装的服务副本，并带上服务的数据目录和地址；未安装服务时指向当前可执行文件并给出警告。Windows 修改用户 PATH 后需打开新终端，cmd 和 PowerShell 均可直接运行 `ccr`。Linux/macOS 若 `~/.local/bin` 不在 PATH 会提示添加（macOS 写入 `~/.zshrc`）。把可执行文件复制或链接为 `ccr` 也有同样效果，数据目录可用 `CC_REMOTE_DATA_DIR` 指定。

## 使用

### 原生终端和手机交替使用（新版）

1. 启动宿主并登录网页，点击「连接原生终端」。
2. 复制页面提供的命令，在需要工作的项目目录中运行。它启动原生 Claude Code，并为这一进程加载本工具的 Channels MCP 桥接组件。
3. 首次运行时，在 Claude Code 原生界面确认开发 Channel 和项目信任。此参数只允许加载指定开发 Channel，不跳过工具权限。
4. 会话自动出现在网页列表；终端和网页均可发送消息，两端使用同一个 CC 进程及上下文。网页退出不会停止终端。
5. 工具审批可以在终端或网页回答，由 Claude Code 接受先到达的有效结果。

默认宿主下的启动示例（在项目目录执行，数据路径替换为实际路径）：

```powershell
& 'D:\Tools\cc-remote\cc-remote.exe' terminal --data-dir 'D:\Tools\cc-remote\data'
```

```sh
/path/to/cc-remote terminal --data-dir /path/to/data
```

对已有会话添加 `--resume <Claude-session-UUID>`，标题从原会话自动读取。首次启用桥接时需先退出旧的 CC 进程，再通过该命令恢复一次；后续终端和手机切换不需要退出或导入。不要同时启动两个相同 ID 的原生 CC 进程。正在运行但未加载 Channels 的普通会话仍只能查看历史。

`terminal` 支持 `--host`（默认 `http://127.0.0.1:8787`）、`--data-dir`、`--claude`、`--claude-config-dir`、`--resume`。网页提供的完整命令会使用当前宿主的数据和配置路径。认证从令牌文件或 `CC_REMOTE_TOKEN` 获取。

### 宿主托管会话

1. 登录页面输入宿主令牌。
2. 点击「新建会话」，输入名称和**宿主上已存在的绝对目录路径**。
3. 发送任务；执行时可以切换到另一个会话继续工作。
4. CC 请求审批或提问时，在会话中的操作卡片回答。
5. 下一条消息使用原 Claude session ID 恢复上下文。

「终端历史」读取 Claude 的本地项目级 JSONL 记录，不修改原文件。共享会话通过 Channels 连接；「导入为托管会话」是另一种运行方式，导入前必须停止原终端。导入沿用原会话标题，无需重新命名。原生历史格式是兼容层，未来 CLI 格式变化可能需要更新解析器。

### 服务状态与会话统计

侧栏底部「服务状态」打开状态面板，每 5 秒刷新：版本、PID、运行时长、监听地址（非本机地址标注「局域网可访问」）、服务托管方式、Claude 版本，会话/待审批计数，数据库、上传附件和日志占用，累计轮次/费用/tokens，近 14 天（UTC）轮次与费用柱状图，可排序的会话统计表（点击行打开会话），以及日志末尾 80 行（自动隐去访问令牌和 Bearer 凭证）。

- `GET /api/status`：上述全部数据，字段包括 `version`、`uptimeSeconds`、`listen`、`listeners[{address,loopback,insecure}]`（`insecure` 表示非本机地址上的明文 HTTP 监听，状态面板会显示警告；网页本身经 http 从非本机地址打开时，主界面也会显示可关闭的明文传输提示）、`service{managed,manager}`、`sessions`、`pendingRequests`、`storage`、`totals`、`daily`、`sessionStats`、`logTail`。`claude --version` 结果缓存 5 分钟，用量统计与上传目录大小缓存 5 秒。
- `GET /api/sessions/{id}/stats`：单个会话的轮次（用户消息数）、tokens、费用、执行耗时、工具调用、错误数和最后活动时间。
- `DELETE /api/sessions/{id}[?stop=1]`：删除会话的 CC Remote 记录（一个事务删除 events、interactions、channel_transcript 与会话行，并删除 `uploads/<id>/`），成功返回 `200 {"status":"deleted"}`；不存在 404；托管轮次执行中返回 409「请先停止执行」，带 `stop=1` 时先停止并等待结束（最多 15 秒，超时 504）；共享终端在线返回 409「共享终端在线，请先退出终端」。
- `POST /api/sessions/{id}/uploads`（multipart 字段 `file`，每次一个）：返回 `{id,kind:"image"|"file",name,mime,size,url}`；超过 20 MiB 或图片内容无效返回 400。`GET` 对应 `url`（需令牌）：图片以原类型 inline 返回，其它文件一律 `Content-Disposition: attachment` 并带 `X-Content-Type-Options: nosniff`，HTML/XML/SVG/脚本类以 `application/octet-stream` 返回。
- `POST /api/admin/shutdown`：仅限本机使用——必须来自回环地址、带令牌，且不得携带 `Forwarded`、`X-Forwarded-For`、`X-Forwarded-Host`、`X-Real-IP` 等代理头（因此经本机反向代理转发的远程请求也会被拒绝），返回 202 后优雅退出。

轮次按用户消息计算；费用、tokens 和耗时取自托管会话的 Claude 结果事件。共享终端的原生记录不含用量，显示为「—」（`costKnown: false`）。

## 设计与限制

- 托管会话每轮由独立 `claude -p` 子进程执行，后续使用精确的 `--resume` ID；共享会话由原生交互式进程长期持有。
- Channels 仍在官方研究预览阶段。自定义桥接使用 `--dangerously-load-development-channels server:cc-remote`，可能受账户/组织政策限制；需要支持 Channels 及审批转发的近期 Claude Code。启动器使用 CC Remote 默认权限模式（未设置时沿用 Claude 用户权限设置），也可显式传入 `--permission-mode`；用户已有工具允许/拒绝规则仍然生效。
- Channel 消息通过 MCP 通知投递，回复通过 reply 工具返回；本地消息与工具过程从原生历史定期同步。它不是原生终端的逐帧镜像，也不保证字符级流式同步。
- 共享会话暂不支持网页停止生成、原生多选提问/计划确认的双端交互；这些仍在原生终端处理。项目信任及 MCP 启用确认也留在终端。
- Channel 没有公开的审批撤销通知。本工具依据后续对应工具结果清理审批，断线/重连/超时也会使旧卡片失效；若终端先回答，网页可能短暂显示旧卡片，最终由 CC 校验请求 ID。
- 消息队列仅存于当前宿主内存，投递确认后清除；连接丢失期间拒绝新消息。宿主重启后不重放旧命令，需要桥接重新注册；投递确认表示写入 MCP 通道，不代表模型已经执行。
- 前端按事件序号每秒拉取增量，提供近实时流式展示；本版尚未使用 WebSocket。
- 审批 MCP 和提问 Hook 通过本机独立监听端口回到宿主，每轮使用独立短期凭证；不修改用户/项目 CC 设置，不默认绕过权限。
- 用户已有的 CC allow/deny 规则仍然生效。并非每个工具调用都会弹出审批。
- 「允许」只同意本次请求。待回答请求最多等待约 30 分钟；停止轮次或重启后失效。
- Windows 停止执行使用进程树终止，Linux 使用进程组终止；已完成的文件修改无法撤销。强制终止后恢复依赖 CC 已落盘的历史。
- 导入原生会话依赖用户确认原进程已停止，本版未自动检测所有原生进程的活跃状态。
- 同目录的多个会话可能修改相同文件。本版不自动创建 worktree。
- 原生历史单文件最多读取 64 MiB，会话发现最多 5000 个文件。大历史、分叉/压缩后的原生记录暂未提供完整分页和分支选择。
- 本版不含推送通知和 Codex 适配器。附件上传后保留在宿主，移除未发送的附件只取消本次附加；删除会话时一并删除其附件，此外尚无自动清理策略。
- PWA 缓存只覆盖静态资源；对话数据和 API 不写入 Service Worker 缓存。不支持离线执行或离线发送队列。

## 开发与验证

前端开发服务器将 `/api` 代理到 `127.0.0.1:8787`：

```sh
pnpm --dir web run dev
```

Web 资源在 Go 编译时嵌入；修改前端后先 `pnpm --dir web run build`，再重新构建宿主。

```sh
go test ./...
go test -race ./...
go vet ./...
pnpm --dir web run typecheck
pnpm --dir web run build
```

自动测试使用假子进程，不消耗模型额度，覆盖事件解析、配置继承、权限桥接、会话并行/串行、取消、持久化、重启状态和数据目录独占。真实模型和浏览器验证记录见 [docs/verification.md](docs/verification.md)。

## 目录

```text
cmd/cc-remote      宿主入口和 hook/MCP 子命令
internal/claude    CC 进程、事件解析、审批桥接、原生历史
internal/channel   原生终端启动器与 Channels MCP 桥接
internal/server    HTTP API、会话执行和交互生命周期
internal/store     SQLite 会话、事件和待回答记录
internal/hostlock  数据目录独占锁
internal/service   系统服务（计划任务/systemd/launchd）与 ccr 别名
internal/listen    多地址监听解析
internal/logfile   宿主日志文件与轮转
web               React/PWA 和嵌入式构建产物
```

接口依据：[Claude CLI](https://code.claude.com/docs/en/cli-reference)、[程序化运行](https://code.claude.com/docs/en/headless)、[Hooks 交互](https://code.claude.com/docs/en/hooks#tools-that-require-user-interaction)。

共享终端中执行 /resume 或 /clear 改变会话 ID 时，会自动断开旧网页绑定，避免向错误会话投递。退出后通过目标会话的连接命令重新连接。同一数据目录中，一个共享会话只允许一个终端启动器。
