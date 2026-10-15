# 工作原理与限制

## 总体结构

CC Remote 由三部分组成，编译为一个可执行文件：

- **Go 宿主**：HTTP API、会话执行与交互生命周期、SQLite 持久化、系统服务管理。
- **React / TypeScript PWA**：构建后嵌入宿主可执行文件。前端按事件序号每秒拉取增量，提供近实时的流式展示（目前未使用 WebSocket）。
- **Claude Code CLI**：宿主直接调用本机已登录的 `claude`，使用结构化消息（stream-json）和审批桥接，而不是终端模拟器或 TTY 镜像。

```text
cmd/cc-remote      宿主入口和 hook / MCP 子命令
internal/claude    Claude Code 进程、事件解析、审批桥接、原生历史
internal/channel   原生终端启动器与 Channels MCP 桥接
internal/server    HTTP API、会话执行和交互生命周期
internal/store     SQLite 会话、事件和待回答记录
internal/prefs     CC Remote 默认偏好（preferences.json）
internal/hostlock  数据目录独占锁
internal/service   系统服务（计划任务 / systemd / launchd）与 ccr 别名
internal/listen    多地址监听解析
internal/logfile   宿主日志文件与轮转
internal/update    自更新
internal/config    默认数据目录
internal/jsonedit  保留格式的 settings.json 原位编辑
web                React / PWA 和嵌入式构建产物
```

接口依据：[Claude CLI](https://code.claude.com/docs/en/cli-reference)、[程序化运行](https://code.claude.com/docs/en/headless)、[Hooks 交互](https://code.claude.com/docs/en/hooks#tools-that-require-user-interaction)。

## 托管会话

- 每轮由独立的 `claude -p` 子进程执行，后续轮次使用精确的 `--resume <ID>` 恢复上下文。
- 审批：通过权限 MCP 和 Hook 把 Claude 的审批请求、AskUserQuestion 提问和 ExitPlanMode 计划确认转成网页卡片。它们经本机独立监听端口回到宿主，每轮使用独立的短期凭证；不修改用户 / 项目的 Claude 设置，不默认绕过权限。
- 用户已有的 allow/deny 规则仍然生效，并非每个工具调用都会弹出审批。「允许」只同意本次请求；待回答请求最多等待约 30 分钟，停止轮次或重启后失效。
- 停止执行：Windows 终止进程树，Linux / macOS 终止进程组；已完成的文件修改无法撤销，强制终止后的恢复依赖 Claude 已落盘的历史。

## 共享终端会话（Channels 桥接）

- `ccr`（`cc-remote -- claude`）启动原生交互式 Claude Code，并通过临时 `--mcp-config` 和 `--dangerously-load-development-channels server:cc-remote` 加载 CC Remote 的 Channels MCP 服务器。
- 网页消息通过 MCP 通知投递给该进程，Claude 通过 reply 工具回复；终端本地的消息与工具过程从原生历史（JSONL）定期同步到网页。它不是终端的逐帧镜像，也不保证字符级流式同步。
- 工具审批经 Channels 的审批转发到网页；终端和网页都可回答，由 Claude Code 接受先到达的有效结果。
- 消息队列只存在于宿主内存，投递确认后清除；连接断开期间拒绝新消息。宿主重启后不重放旧命令，桥接需要重新注册。投递确认表示已写入 MCP 通道，不代表模型已经执行。
- 连接按代际隔离：会话 ID 变化（`/resume`、`/clear`，由 SessionStart Hook 检测）时断开旧绑定，避免投递到错误的会话。

### 提问与计划确认（PermissionRequest Hook）

启动器在临时 `--settings` 中注册 `PermissionRequest` Hook（matcher `AskUserQuestion|ExitPlanMode`，命令 `cc-remote channel-ask-hook`，超时 1800 秒）。依据 Claude Code 2.1.300 的实测行为：

- 该 Hook 与原生对话框**并行**运行，终端对话框始终可操作。
- 网页先答：Hook 输出决定。提问为 `updatedInput.answers`（多选标签以 `, ` 连接，可填自由文本）；批准计划时附 `setMode`（自动接受编辑 / 手动审批编辑 / 自动模式），或带修改意见「继续规划」。
- 终端先答：Hook 轮询 transcript 发现对应 tool_result 后静默退出，网页卡片标记「已在终端回答」。
- 「交给终端」撤回网页卡片，只在终端作答。
- Hook 一律 fail-open：会话不匹配、宿主不可达或任何错误都不输出决定，原生对话框照常可用。
- Hook 连接宿主所需的令牌写在启动器临时配置目录的 0600 文件中（经 `CC_REMOTE_ASK_FILE` 定位），不进入环境变量。

## 持久化与恢复

- SQLite 保存会话、事件和待回答记录；刷新页面或关闭浏览器不会停止任务。
- 一个数据目录同时只允许一个宿主（独占锁）。宿主重启时把执行中的轮次标记为中断、待回答请求标记为过期，不会自动重放。
- 「终端历史」读取 Claude Code 的项目级 JSONL，只读；原生历史格式是兼容层，未来 CLI 格式变化可能需要更新解析器。

## 已知限制

- **Channels 处于官方研究预览阶段**。开发 Channel 参数可能受账户 / 组织政策限制；需要支持 Channels 及审批转发的近期 Claude Code。实际集成在 Claude Code 2.1.299 / 2.1.300 上验证，旧版本不保证兼容。
- 共享会话暂不支持网页停止生成；项目信任、MCP 启用确认也留在终端。
- 共享会话的提问 / 计划卡片依赖 Hook 与原生对话框并行的行为；设置了 `disableAllHooks` 或组织 `allowManagedHooksOnly` 时只能在终端作答，Claude Code 后续若改变该行为也可能失效。
- Channel 没有公开的审批撤销通知。审批依据后续对应的工具结果清理，断线 / 重连 / 超时也会使旧卡片失效；终端先回答时网页可能短暂显示旧卡片，最终由 Claude Code 校验请求 ID。
- 共享终端只能依据连接状态和收到的审批请求显示状态，无法精确显示终端本地每一轮的执行阶段；原生记录不含用量。
- 导入原生会话依赖用户确认原进程已停止，未自动检测所有原生进程。
- 同一目录的多个会话可能修改相同文件，目前不自动创建 worktree。
- 原生历史单文件最多读取 64 MiB，会话发现最多 5000 个文件；大历史、分叉 / 压缩后的记录暂无完整分页和分支选择。
- 附件没有自动清理策略（删除会话时一并删除）。
- 不含推送通知和 Codex 适配器。PWA 只缓存静态资源，不支持离线执行或离线发送队列。
