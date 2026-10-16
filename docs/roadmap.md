# 路线图：补全会话模式

目标：会话（托管）模式能完整覆盖日常使用，包括子 agent 的查看与处理、上下文压缩、新上下文等；共享模式维持现状。

## 依据（Claude Code 2.1.300 实测）

- `-p --input-format stream-json --output-format stream-json` 可作为常驻进程：同一进程内连续多条消息、`interrupt` 控制请求打断当前轮（进程保留，响应含 `still_queued`）、发送 `/compact`、`set_model` 控制请求即时切换模型，压缩与切换后上下文保持。
- 子 agent 内部的消息带 `parent_tool_use_id`（指向发起它的 Agent 工具调用）；另有 `task_started`、`task_progress`（描述、`last_tool_name`）、`task_updated`、`task_notification`（完成状态）、`background_tasks_changed` 系统事件。
- `-p` 模式的 init 事件 `slash_commands` 列出可执行命令（已用于命令透传）；`/clear` 在 `-p` 下会换会话 ID（`conversation_reset`）。

## 阶段 1：常驻进程运行时（已完成）

- 每个活跃托管会话一个常驻 Claude 进程，空闲超时退出，下次用 `--resume` 拉起；进程意外退出时自动恢复。
- 停止改为 `interrupt` 控制请求，不再结束进程。
- 模型、推理强度、权限模式变化后，空闲时下一条消息重启进程生效（`set_model`、`set_permission_mode` 实测可用，留作运行中即时切换的后续优化）。
- 运行中发送的消息由 cc-remote 排队、逐条发送（Claude 会合并 stdin 上排队的多条消息），界面显示等待状态。
- 审批、提问、计划确认等现有桥接保持不变。

## 阶段 2：子 agent 与后台任务

- Agent 工具调用渲染为可展开卡片，内部按 `parent_tool_use_id` 嵌套子 agent 的消息与工具调用，运行中显示 `task_progress`。
- 后台任务面板：运行中与已完成的任务及状态。

## 阶段 3：日常功能

- TodoWrite 任务清单面板、上下文用量提示、每轮耗时与用量。
- 会话重命名与 Claude 同步。

## 阶段 4：与终端交接

- 「在终端继续」：结束常驻进程并给出 `claude --resume <id>`，网页端加锁防止双写；「接回网页」恢复托管。

## 暂不做

- 共享模式的伪终端中转（ConPTY / pty）。网页消息经 Channels 送达，Claude Code 不会当作命令执行；若以后需要，只评估「终端空闲时插入 `/compact`、`/clear`」的最小范围。
