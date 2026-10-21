# 工作原理与限制

## 总体结构

CC Remote 由三部分组成，编译为一个可执行文件：

- **Go 宿主**：HTTP API、会话执行与交互生命周期、SQLite 持久化、系统服务管理。
- **React / TypeScript PWA**：构建后嵌入宿主可执行文件。前端按事件序号每秒拉取增量，提供近实时的流式展示（目前未使用 WebSocket）。
- **Claude Code CLI**：宿主直接调用本机已登录的 `claude`，使用结构化消息（stream-json）和审批桥接，而不是终端模拟器或 TTY 镜像。

```text
cmd/cc-remote      宿主入口和 hook / MCP 子命令
internal/claude    Claude Code 进程、事件解析、审批桥接、原生历史
internal/channel   终端会话的提问 / 计划 Hook（channel-ask-hook）与 ccr 拉起宿主
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

- 每个活跃会话保持一个常驻的 `claude -p --input-format stream-json --output-format stream-json --replay-user-messages` 进程。网页消息到达即写入 stdin（每个会话一个写入协程，多个客户端同时发送也按顺序写入），运行中发送的消息由 Claude 自己排队，并在下一轮合并处理（与终端里排队输入的行为一致）。
- 每轮开始时 Claude 回显本轮处理的消息（合并的消息以换行连接一起回显），cc-remote 据此把消息逐条记入聊天记录；回显前网页显示为「等待处理」。斜杠命令在输出之后才回显，因此以本轮第一条输出为准提前记录。`result` 的 `queued_turn_count` 实测不可靠，不用于判断。
- 会话忙碌期间（Claude 在一轮中，包括它自行发起的轮次——例如后台 agent 完成后的汇报，`result` 带 `origin`；有已发送但尚未回显的消息；或有未回答的审批请求，包括轮次之间后台 agent 发起的审批）状态为执行中。空闲 30 分钟且无后台任务时进程退出，下条消息用 `--resume <ID>` 拉起；模型、推理强度、权限模式变化后，空闲时下一条消息也会重启进程。
- 设置环境变量 `CC_REMOTE_ONESHOT=1` 可回退为每轮一个 `claude -p` 进程的旧模式。
- 子 agent：子 agent 内部的消息带 `parent_tool_use_id`，宿主把它写进事件 payload，网页按它把这些消息嵌套到对应的 Agent 调用卡片里；这些输出不算作会话的一轮，后台 agent 在会话空闲时继续工作不会让会话变成执行中。`task_started`、`task_progress`、`task_updated`、`task_notification` 系统事件由 `/api/sessions/{id}/tasks` 按任务汇总（状态、进度、工具次数、tokens、耗时、最终报告），宿主进程已不在而任务仍显示运行中的，报告为已中断。终端历史和终端会话的子 agent 过程记录在单独的文件里，目前只显示调用和最终结果。
- 审批：通过权限 MCP 和 Hook 把 Claude 的审批请求、AskUserQuestion 提问和 ExitPlanMode 计划确认转成网页卡片。它们经本机独立监听端口回到宿主，每轮使用独立的短期凭证；不修改用户 / 项目的 Claude 设置，不默认绕过权限。
- 用户已有的 allow/deny 规则仍然生效，并非每个工具调用都会弹出审批。「允许」只同意本次请求；待回答请求最多等待约 30 分钟，停止轮次或重启后失效。
- 停止执行：发送 `interrupt` 控制请求，只中断当前轮，进程和上下文保留，排队的消息继续执行；再次停止，或中断后 15 秒仍未结束，则结束进程；删除会话、`/clear`、宿主退出时结束进程（Windows 终止进程树，Linux / macOS 终止进程组）。已完成的文件修改无法撤销。

## 终端会话的提问与计划确认（PermissionRequest Hook）

终端会话由宿主在伪终端中运行交互式 Claude Code（见 [design/terminal-sessions.md](design/terminal-sessions.md)），网页从原生记录（JSONL）同步对话。宿主在会话的 `--settings` 中注册 `PermissionRequest` Hook（matcher `*`：工具审批、AskUserQuestion 和 ExitPlanMode，命令 `cc-remote channel-ask-hook`，超时约 7 天，见 `claude.AskWait`：离开很久后回来仍能在网页作答；宿主先于 Hook 放弃，超时记为「已超时」而不是「已在终端回答」）。Hook 长轮询 `POST /api/channels/{Claude 会话 ID}/interactions`，网页经 `POST /api/requests/{id}` 回答。依据 Claude Code 2.1.300 的实测行为：

- 该 Hook 与原生对话框**并行**运行，终端对话框始终可操作。
- 网页先答：Hook 输出决定。提问为 `updatedInput.answers`（多选标签以 `, ` 连接，可填自由文本）；批准计划时附 `setMode`（自动接受编辑 / 手动审批编辑 / 自动模式），或带修改意见「继续规划」。
- 网页批准工具时 Hook 输出 `allow`，拒绝时输出 `deny` 和说明。
- 终端先答：Hook 轮询 transcript 发现对应 tool_result 后静默退出，网页卡片标记「已在终端回答」。工具调用按工具名和两边都有的输入字段对应；子 agent 的调用（Hook 输入带 `agent_id`）读 `<会话>/subagents/agent-<id>.jsonl`，因为后台子 agent 的对话框不会立即显示在主界面上，主界面的屏幕不能说明它已被回答。
- 终端批准普通工具后，Claude Code 先执行工具，等 Hook 结束才继续，执行期间 transcript 不写任何内容，tool_result 要等工具跑完。所以主 agent 的审批另由宿主看屏幕：对话框出现过、又离开屏幕 2 秒，就撤回网页卡片（子 agent 的审批不用这个办法）。
- 宿主的兜底只认审批对应的那次调用：建卡时 transcript 里唯一一个尚无结果的同名同输入调用；有多个这样的调用（相同调用并行）或是子 agent 的调用时不兜底，交给 Hook。网页回答一个审批后，同一会话还有别的审批在等时，会话保持「等待操作」。
- 「交给终端」撤回网页卡片，只在终端作答。
- Hook 一律 fail-open：会话不匹配、宿主不可达或任何错误都不输出决定，原生对话框照常可用。
- Hook 连接宿主所需的令牌写在会话配置目录的 0600 文件 `ask.json` 中（经 `CC_REMOTE_ASK_FILE` 定位），不进入环境变量。

早期的共享终端模式（Claude Code 在用户自己的终端里运行，网页经 Channels MCP 桥接）已移除；数据库中的共享会话在宿主启动时转为终端会话，沿用原 Claude 会话 ID，下次使用时以 `--resume` 恢复。

## 持久化与恢复

- SQLite 保存会话、事件和待回答记录；刷新页面或关闭浏览器不会停止任务。
- 一个数据目录同时只允许一个宿主（独占锁）。宿主重启时把执行中的轮次标记为中断、待回答请求标记为过期，不会自动重放。
- 「终端历史」读取 Claude Code 的项目级 JSONL，只读；原生历史格式是兼容层，未来 CLI 格式变化可能需要更新解析器。列表按文件大小和修改时间缓存每个 transcript 的元数据，只重读有变化的文件；打开的原生会话缓存解析结果（最近 8 个），文件增长时从上次读到的完整行之后增量解析。

## 加载与传输

- 会话从末尾分页加载：首屏只取最后约 200 个事件（`?tail=`），向上滚动到顶部附近时再取更早的一页（`/events?before=`），不往上翻就不加载。分页起点对齐到 100 个事件内最近的用户消息，找不到时对齐到最近的非工具结果事件，避免把工具调用和结果拆到两页。
- 列表接口带 `compact=1` 时，工具调用与结果中超过 2000 字节的字符串会被截断并标记 `truncated`；网页展开该工具调用时再按 `/events/{seq}` 取完整内容。提问与计划确认工具不截断。
- GET 响应带弱 ETag，轮询未变化时返回 304 不传正文；大于 1 KiB 的 JSON 和静态资源按 gzip 压缩。网页把会话列表缓存在本地（退出登录时清除），刷新后立即显示，再在后台更新。

## 已知限制

- 终端会话的提问 / 计划卡片依赖 Hook 与原生对话框并行的行为；设置了 `disableAllHooks` 或组织 `allowManagedHooksOnly` 时只能在终端作答，Claude Code 后续若改变该行为也可能失效。
- 终端会话的原生记录不含用量，状态页的费用与 tokens 显示为「—」。
- 导入原生会话依赖用户确认原进程已停止，未自动检测所有原生进程。
- 同一目录的多个会话可能修改相同文件，目前不自动创建 worktree。
- 原生历史单文件最多读取 64 MiB，会话发现最多 5000 个文件；大历史、分叉 / 压缩后的记录暂无完整分页和分支选择。
- 附件没有自动清理策略（删除会话时一并删除）。
- 不含 Web Push 推送（页面关闭后的通知）和 Codex 适配器；页面打开时的系统通知由前端轮询会话列表产生。PWA 只缓存静态资源，不支持离线执行或离线发送队列。
