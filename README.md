# CC Remote

Go 宿主 + React/TypeScript PWA + SQLite 的 Claude Code 远程界面。直接运行本机的 Claude Code CLI，使用结构化消息和审批桥接，无需终端模拟器。

## 当前功能

开发任务、完成状态和续接边界见 [docs/tasks.md](docs/tasks.md)。

- 左侧列表管理会话：打开过的会话显示「已缓存」，切换保留消息、草稿、截图、滚动位置、工具展开状态和未提交的回答；点击「×」释放缓存或刷新网页会清除未发送草稿，已发送消息仍由宿主保存，宿主任务继续执行。
- 紧凑布局与可折叠侧栏；Enter / Ctrl+Enter 发送方式可选，Shift+Enter 换行，兼容中文输入法。
- 令牌记在当前站点 localStorage，刷新及重新打开网页无需重登；退出清除。
- 选择或粘贴截图：PNG/JPEG/GIF，每张最多 10 MiB、2500 万像素，每条消息最多 8 张。图片由宿主保存，通过原生 Read 工具交给 Claude，可能需要一次读取审批。
- Claude 用户级模型、推理强度和权限默认设置面板；保留原文件其它设置，不暴露环境变量等敏感字段。
- 网页 `/help`、`/status`、`/config`、`/permissions`、`/model`、`/effort`；托管会话额外支持 `/clear` 和 `/stop`。完整原生控制命令仍在终端执行，网页会明确提示。
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

原生 `--model`、`--permission-mode`、精确 UUID 的 `--resume/-r` 和 `--session-id` 可透传。为保证共享绑定，暂不接受 `--continue`、`--fork-session`、`-p`、覆盖 `--settings/--mcp-config/--channels`。旧的 `terminal` 子命令继续支持。

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
- Node.js 22 / npm，用于构建前端；运行已构建宿主无需 Node。
- Windows 或 Linux；安装近期版本的 Claude Code，并在宿主执行 `claude auth login`。
- 本版使用当前 CLI 的 exec-form hooks 和 permission host。实际集成在 Claude Code 2.1.299 上验证；旧版本不保证兼容。

## 构建与运行

在项目根目录执行：

```sh
npm --prefix web ci
npm --prefix web run build
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

如果希望通过远程 IP 访问，启动时设置监听地址：

```powershell
.\cc-remote.exe --listen 0.0.0.0:8787
```

```sh
./cc-remote --listen 0.0.0.0:8787
```

也可以通过 `CC_REMOTE_TOKEN` 环境变量设置至少 16 字符的访问令牌。令牌持有者可以读取会话，并通过 CC 操作宿主工作目录。远程连接的加密由你的 HTTPS 反向代理或私有网络提供。通过 HTTP 远程 IP 可以使用 Web 界面；PWA 的安装与 Service Worker 通常要求 HTTPS（localhost 除外）。

CLI 参数：

| 参数 | 默认值 | 用途 |
| --- | --- | --- |
| `--listen` | `127.0.0.1:8787` | Web/API 监听地址 |
| `--data-dir` | 用户配置目录下的 `cc-remote` | SQLite、令牌、图片和宿主锁目录；可由 `CC_REMOTE_DATA_DIR` 改变默认值 |
| `--claude` | `claude` | Claude Code 可执行文件路径 |
| `--claude-config-dir` | `CLAUDE_CONFIG_DIR` 或 `~/.claude` | 原生历史发现和实际执行使用的同一配置目录 |

一个数据目录同时只允许一个宿主运行。重启宿主会把仍在运行的轮次标记为中断，并使旧审批失效，不会自动重放命令。

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

## 设计与限制

- 托管会话每轮由独立 `claude -p` 子进程执行，后续使用精确的 `--resume` ID；共享会话由原生交互式进程长期持有。
- Channels 仍在官方研究预览阶段。自定义桥接使用 `--dangerously-load-development-channels server:cc-remote`，可能受账户/组织政策限制；需要支持 Channels 及审批转发的近期 Claude Code。启动器沿用用户权限设置，也可显式传入 `--permission-mode`；用户已有工具允许/拒绝规则仍然生效。
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
- 本版不含推送通知和 Codex 适配器。图片上传后保留在宿主，删除未发送的缩略图只取消本次附加；尚无自动清理图片策略。
- PWA 缓存只覆盖静态资源；对话数据和 API 不写入 Service Worker 缓存。不支持离线执行或离线发送队列。

## 开发与验证

前端开发服务器将 `/api` 代理到 `127.0.0.1:8787`：

```sh
npm --prefix web run dev
```

Web 资源在 Go 编译时嵌入；修改前端后先 `npm --prefix web run build`，再重新构建宿主。

```sh
go test ./...
go test -race ./...
go vet ./...
npm --prefix web run typecheck
npm --prefix web run build
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
web               React/PWA 和嵌入式构建产物
```

接口依据：[Claude CLI](https://code.claude.com/docs/en/cli-reference)、[程序化运行](https://code.claude.com/docs/en/headless)、[Hooks 交互](https://code.claude.com/docs/en/hooks#tools-that-require-user-interaction)。

共享终端中执行 /resume 或 /clear 改变会话 ID 时，会自动断开旧网页绑定，避免向错误会话投递。退出后通过目标会话的连接命令重新连接。同一数据目录中，一个共享会话只允许一个终端启动器。
